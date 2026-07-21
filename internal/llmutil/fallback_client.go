package llmutil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
)

var fallbackStatusPattern = regexp.MustCompile(`\bstatus(?:\s+code)?\s+(\d{3})\b`)

type FallbackCandidate struct {
	Profile string
	Model   string
	Client  llm.Client
}

type FallbackClientOptions struct {
	Primary        llm.Client
	PrimaryProfile string
	PrimaryModel   string
	Fallbacks      []FallbackCandidate
	Logger         *slog.Logger
}

type fallbackClient struct {
	primary        llm.Client
	primaryProfile string
	primaryModel   string
	fallbacks      []FallbackCandidate
	logger         *slog.Logger
}

func NewFallbackClient(opts FallbackClientOptions) llm.Client {
	if opts.Primary == nil {
		return nil
	}
	fallbacks := make([]FallbackCandidate, 0, len(opts.Fallbacks))
	for _, fallback := range opts.Fallbacks {
		if fallback.Client == nil {
			continue
		}
		fallback.Profile = strings.TrimSpace(fallback.Profile)
		fallback.Model = strings.TrimSpace(fallback.Model)
		fallbacks = append(fallbacks, fallback)
	}
	if len(fallbacks) == 0 {
		return opts.Primary
	}
	return &fallbackClient{
		primary:        opts.Primary,
		primaryProfile: strings.TrimSpace(opts.PrimaryProfile),
		primaryModel:   strings.TrimSpace(opts.PrimaryModel),
		fallbacks:      fallbacks,
		logger:         opts.Logger,
	}
}

func (c *fallbackClient) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil || c.primary == nil {
		return llm.Result{}, errors.New("fallback client is not initialized")
	}
	result, err := c.primary.Chat(ctx, req)
	if err == nil {
		return result, nil
	}
	reason, ok := fallbackEligibleReason(err)
	if !ok {
		return llm.Result{}, err
	}
	c.logFallback("llm_profile_fallback_triggered", 0, "", "", reason, err)

	lastErr := err
	for idx, fallback := range c.fallbacks {
		fallbackReq := req
		fallbackReq.Model = fallback.Model

		result, err = fallback.Client.Chat(ctx, fallbackReq)
		if err == nil {
			c.logFallback("llm_profile_fallback_succeeded", idx+1, fallback.Profile, fallback.Model, reason, lastErr)
			return result, nil
		}
		lastErr = err
		nextReason, transient := fallbackEligibleReason(err)
		if transient {
			c.logFallback("llm_profile_fallback_candidate_failed", idx+1, fallback.Profile, fallback.Model, nextReason, err)
		} else {
			c.logFallback("llm_profile_fallback_candidate_failed", idx+1, fallback.Profile, fallback.Model, "non_transient", err)
		}
	}
	c.logFallback("llm_profile_fallback_exhausted", len(c.fallbacks), "", "", reason, lastErr)
	return llm.Result{}, lastErr
}

func (c *fallbackClient) Close() error {
	if c == nil {
		return nil
	}
	clients := make([]llm.Client, 0, 1+len(c.fallbacks))
	clients = append(clients, c.primary)
	for _, fallback := range c.fallbacks {
		clients = append(clients, fallback.Client)
	}
	return closeDistinctClients(clients...)
}

func (c *fallbackClient) logFallback(event string, attempt int, profile string, model string, reason string, err error) {
	if c == nil || c.logger == nil {
		return
	}
	args := []any{
		"attempt", attempt,
		"primary_profile", c.primaryProfile,
		"primary_model", c.primaryModel,
		"reason", reason,
	}
	if profile = strings.TrimSpace(profile); profile != "" {
		args = append(args, "fallback_profile", profile)
	}
	if model = strings.TrimSpace(model); model != "" {
		args = append(args, "fallback_model", model)
	}
	if err != nil {
		args = append(args, "error", err.Error())
	}
	c.logger.Warn(event, args...)
}

func fallbackEligibleReason(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout", true
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if status, ok := fallbackHTTPStatus(msg); ok {
		if reason, eligible := fallbackStatusReason(status); eligible {
			return reason, true
		}
	}

	switch {
	case strings.Contains(msg, "too many requests"),
		strings.Contains(msg, "rate limit"):
		return "status_429", true
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "client.timeout exceeded"),
		strings.Contains(msg, "timeout exceeded while awaiting headers"),
		strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "request timeout"),
		strings.Contains(msg, "deadline exceeded"),
		strings.Contains(msg, "timed out"):
		return "timeout", true
	default:
		return "", false
	}
}

func fallbackHTTPStatus(msg string) (int, bool) {
	if msg = strings.TrimSpace(msg); msg == "" {
		return 0, false
	}
	match := fallbackStatusPattern.FindStringSubmatch(msg)
	if len(match) != 2 {
		return 0, false
	}
	status, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return status, true
}

func fallbackStatusReason(status int) (string, bool) {
	switch {
	case status == 401, status == 403, status == 404, status == 408, status == 415, status == 422, status == 429:
		return fmt.Sprintf("status_%d", status), true
	case status >= 500 && status <= 599:
		return fmt.Sprintf("status_%d", status), true
	default:
		return "", false
	}
}
