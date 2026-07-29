package xaioauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/quailyquaily/mistermorph/internal/xaiauth"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiProvider "github.com/quailyquaily/mistermorph/providers/uniai"
	uniaiapi "github.com/quailyquaily/uniai"
)

var (
	ErrUnauthorized = errors.New("xAI OAuth authorization was rejected; run `mistermorph auth xai login` to sign in again")
	ErrEntitlement  = errors.New("xAI OAuth inference is unavailable for this account; subscription entitlement, region, or team policy may not allow it")
)

type Config struct {
	Model   string
	Headers map[string]string

	RequestTimeout     time.Duration
	Temperature        *float64
	ReasoningEffort    string
	ToolsEmulationMode string
	StateDir           string
	OAuth              xaiauth.OAuthConfig
}

type Client struct {
	cfg Config
}

func New(cfg Config) *Client {
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Model == "" {
		cfg.Model = xaiauth.DefaultModel
	}
	cfg.Headers = sanitizeHeaders(cfg.Headers)
	cfg.ReasoningEffort = strings.TrimSpace(cfg.ReasoningEffort)
	cfg.ToolsEmulationMode = strings.TrimSpace(cfg.ToolsEmulationMode)
	cfg.StateDir = strings.TrimSpace(cfg.StateDir)
	if cfg.Temperature != nil {
		temperature := *cfg.Temperature
		cfg.Temperature = &temperature
	}
	return &Client{cfg: cfg}
}

func (c *Client) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil {
		return llm.Result{}, fmt.Errorf("xAI OAuth provider is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.cfg.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.RequestTimeout)
		defer cancel()
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = c.cfg.Model
	}
	if strings.TrimSpace(req.InferenceProvider) == "" {
		req.InferenceProvider = xaiauth.ProviderName
	}
	token, err := xaiauth.ResolveToken(ctx, c.cfg.StateDir, c.cfg.OAuth)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return llm.Result{}, ctxErr
		}
		return llm.Result{}, err
	}
	result, err := c.chatWithToken(ctx, req, token.AccessToken)
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		return sanitizeInferenceResultError(result, err, req.Model)
	}

	refreshed, refreshErr := xaiauth.RefreshRejectedToken(
		ctx,
		c.cfg.StateDir,
		c.cfg.OAuth,
		token.AccessToken,
	)
	if refreshErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return llm.Result{}, ctxErr
		}
		return llm.Result{}, fmt.Errorf("%w; refresh failed: %v", ErrUnauthorized, refreshErr)
	}
	result, err = c.chatWithToken(ctx, req, refreshed.AccessToken)
	apiErr = nil
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
		return llm.Result{}, ErrUnauthorized
	}
	return sanitizeInferenceResultError(result, err, req.Model)
}

func (c *Client) chatWithToken(ctx context.Context, req llm.Request, accessToken string) (llm.Result, error) {
	req.OnStream = withoutStreamCost(req.OnStream)

	base, err := uniaiProvider.New(uniaiProvider.Config{
		Provider:           "openai_resp",
		InferenceProvider:  xaiauth.ProviderName,
		Endpoint:           xaiauth.DefaultAPIBase,
		APIKey:             strings.TrimSpace(accessToken),
		Model:              c.cfg.Model,
		Headers:            c.cfg.Headers,
		Pricing:            &uniaiapi.PricingCatalog{},
		RequestTimeout:     c.cfg.RequestTimeout,
		Temperature:        c.cfg.Temperature,
		CacheTTL:           "off",
		ToolsEmulationMode: c.cfg.ToolsEmulationMode,
		ReasoningEffort:    c.cfg.ReasoningEffort,
	})
	if err != nil {
		return llm.Result{}, err
	}
	result, err := base.Chat(ctx, req)
	result.Usage.Cost = nil
	return result, err
}

func sanitizeInferenceResultError(result llm.Result, err error, model string) (llm.Result, error) {
	result.Usage.Cost = nil
	if err == nil {
		return result, nil
	}
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return llm.Result{}, err
	}
	switch apiErr.StatusCode {
	case http.StatusUnauthorized:
		return llm.Result{}, ErrUnauthorized
	case http.StatusForbidden:
		return llm.Result{}, ErrEntitlement
	case http.StatusTooManyRequests:
		if retryAfter := formatRetryAfter(apiErr.Response); retryAfter != "" {
			return llm.Result{}, fmt.Errorf("xAI OAuth inference was rate limited (HTTP 429); retry after %s", retryAfter)
		}
		return llm.Result{}, fmt.Errorf("xAI OAuth inference was rate limited (HTTP 429)")
	case http.StatusNotFound:
		return llm.Result{}, fmt.Errorf("xAI OAuth model %q is unavailable for this account; select an available model", strings.TrimSpace(model))
	default:
		return llm.Result{}, fmt.Errorf("xAI OAuth inference failed with HTTP %d", apiErr.StatusCode)
	}
}

func formatRetryAfter(response *http.Response) string {
	if response == nil {
		return ""
	}
	if raw := strings.TrimSpace(response.Header.Get("Retry-After-Ms")); raw != "" {
		if milliseconds, err := strconv.ParseFloat(raw, 64); err == nil && milliseconds > 0 {
			return (time.Duration(milliseconds * float64(time.Millisecond))).String()
		}
	}
	raw := strings.TrimSpace(response.Header.Get("Retry-After"))
	if raw == "" {
		return ""
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds > 0 {
		return (time.Duration(seconds * float64(time.Second))).String()
	}
	if deadline, err := http.ParseTime(raw); err == nil {
		wait := time.Until(deadline)
		if wait > 0 {
			return wait.Round(time.Second).String()
		}
	}
	return ""
}

func withoutStreamCost(handler llm.StreamHandler) llm.StreamHandler {
	if handler == nil {
		return nil
	}
	return func(event llm.StreamEvent) error {
		if event.Usage != nil {
			usage := *event.Usage
			usage.Cost = nil
			event.Usage = &usage
		}
		return handler(event)
	}
}

func sanitizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "authorization", "proxy-authorization", "x-api-key", "api-key":
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
