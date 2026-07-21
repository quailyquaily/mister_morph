package uniai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
)

type Config struct {
	Provider          string
	InferenceProvider string
	Endpoint          string
	APIKey            string
	Model             string
	Headers           map[string]string
	Pricing           *uniaiapi.PricingCatalog

	RequestTimeout  time.Duration
	Temperature     *float64
	ReasoningEffort string
	ReasoningBudget *int
	CacheTTL        string
	CacheKeyPrefix  string

	ToolsEmulationMode  string
	AzureAPIKey         string
	AzureEndpoint       string
	AzureDeployment     string
	AwsKey              string
	AwsSecret           string
	AwsSessionToken     string
	AwsProfile          string
	AwsRegion           string
	AwsBedrockModelArn  string
	CloudflareAccountID string
	CloudflareAPIToken  string
	CloudflareAPIBase   string

	Debug bool
}

type Client struct {
	provider           string
	inferenceProvider  string
	model              string
	pricing            *uniaiapi.PricingCatalog
	requestTimeout     time.Duration
	temperature        *float64
	reasoningEffort    string
	reasoningBudget    *int
	cacheTTL           string
	cacheKeyPrefix     string
	toolsEmulationMode uniaiapi.ToolsEmulationMode
	client             *uniaiapi.Client
}

func New(cfg Config) (*Client, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	pricing := cfg.Pricing
	if pricing == nil {
		pricing = uniaiapi.DefaultPricingCatalog()
	}

	if provider == "bedrock" {
		if err := ResolveBedrockCredentials(context.Background(), &cfg); err != nil {
			return nil, fmt.Errorf("resolve bedrock credentials: %w", err)
		}
	}

	openAIBase := normalizeOpenAIBase(cfg.Endpoint)
	openAIKey := strings.TrimSpace(cfg.APIKey)

	azureAPIKey := firstNonEmpty(cfg.AzureAPIKey, cfg.APIKey)
	azureEndpoint := firstNonEmpty(cfg.AzureEndpoint, cfg.Endpoint)
	azureDeployment := firstNonEmpty(cfg.AzureDeployment, cfg.Model)

	anthropicKey := strings.TrimSpace(cfg.APIKey)
	anthropicModel := strings.TrimSpace(cfg.Model)

	geminiKey := strings.TrimSpace(cfg.APIKey)
	geminiBase := strings.TrimSpace(cfg.Endpoint)

	uCfg := uniaiapi.Config{
		Provider:            provider,
		OpenAIAPIKey:        openAIKey,
		OpenAIAPIBase:       openAIBase,
		OpenAIModel:         strings.TrimSpace(cfg.Model),
		ChatHeaders:         cloneStringMap(cfg.Headers),
		AzureOpenAIAPIKey:   strings.TrimSpace(azureAPIKey),
		AzureOpenAIEndpoint: strings.TrimSpace(azureEndpoint),
		AzureOpenAIModel:    strings.TrimSpace(azureDeployment),
		AnthropicAPIKey:     strings.TrimSpace(anthropicKey),
		AnthropicModel:      strings.TrimSpace(anthropicModel),
		AwsKey:              strings.TrimSpace(cfg.AwsKey),
		AwsSecret:           strings.TrimSpace(cfg.AwsSecret),
		AwsSessionToken:     strings.TrimSpace(cfg.AwsSessionToken),
		AwsRegion:           strings.TrimSpace(cfg.AwsRegion),
		AwsBedrockModelArn:  strings.TrimSpace(cfg.AwsBedrockModelArn),
		CloudflareAccountID: strings.TrimSpace(cfg.CloudflareAccountID),
		CloudflareAPIToken:  strings.TrimSpace(cfg.CloudflareAPIToken),
		CloudflareAPIBase:   strings.TrimSpace(cfg.CloudflareAPIBase),
		GeminiAPIKey:        strings.TrimSpace(geminiKey),
		GeminiAPIBase:       strings.TrimSpace(geminiBase),
		Pricing:             pricing,

		Debug: cfg.Debug,
	}

	return &Client{
		provider:           provider,
		inferenceProvider:  strings.TrimSpace(cfg.InferenceProvider),
		model:              strings.TrimSpace(cfg.Model),
		pricing:            pricing,
		requestTimeout:     cfg.RequestTimeout,
		temperature:        cloneFloat64(cfg.Temperature),
		reasoningEffort:    strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort)),
		reasoningBudget:    cloneInt(cfg.ReasoningBudget),
		cacheTTL:           strings.TrimSpace(cfg.CacheTTL),
		cacheKeyPrefix:     strings.TrimSpace(cfg.CacheKeyPrefix),
		toolsEmulationMode: normalizeToolsEmulationMode(cfg.ToolsEmulationMode),
		client:             uniaiapi.New(uCfg),
	}, nil
}

func (c *Client) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	start := time.Now()
	if c.requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		defer cancel()
	}
	if strings.TrimSpace(req.InferenceProvider) == "" && c.inferenceProvider != "" {
		req.InferenceProvider = c.inferenceProvider
	}
	streamDebug := newStreamDebugCapture(c.provider, req.DebugFn, req.OnStream != nil && supportsStreaming(c.provider))
	if streamDebug != nil {
		req.OnStream = streamDebug.Wrap(req.OnStream)
	}
	opts := c.buildChatOptions(req, req.ForceJSON)
	resp, err := c.client.Chat(ctx, opts...)
	if err != nil {
		streamDebug.EmitPartial(err)
		c.emitChatError(req.DebugFn, err, req.ForceJSON, 1)
	}
	if err != nil && req.ForceJSON && shouldRetryWithoutResponseFormat(err) {
		streamDebug.Reset()
		opts = c.buildChatOptions(req, false)
		resp, err = c.client.Chat(ctx, opts...)
		if err != nil {
			streamDebug.EmitPartial(err)
			c.emitChatError(req.DebugFn, err, false, 2)
		}
	}
	if err != nil {
		return llm.Result{}, err
	}
	if resp == nil {
		err = fmt.Errorf("uniai: empty response")
		c.emitChatError(req.DebugFn, err, req.ForceJSON, 0)
		return llm.Result{}, err
	}
	streamDebug.EmitResponse(resp)

	toolCalls := toLLMToolCalls(resp.ToolCalls)
	model := firstNonEmpty(req.Model, c.model)
	usage := toLLMUsage(resp.Usage)
	if providerUsesOpenAICompatibleUsage(c.provider) {
		if enriched, changed := enrichUsageFromOpenAICompatibleRaw(usage, resp.Raw); changed {
			usage = recalculateUsageCost(enriched, c.pricing, req.InferenceProvider, model)
		}
	}
	if shouldEnsureGeminiThoughtSignature(c.provider, model) {
		toolCalls = ensureGeminiToolCallThoughtSignatures(toolCalls)
	}

	return llm.Result{
		Text:      resp.Text,
		Parts:     toLLMParts(resp.Parts),
		Messages:  toLLMMessages(uniaiapi.AssistantReplayMessages(resp)),
		ToolCalls: toolCalls,
		Usage:     usage,
		Duration:  time.Since(start),
	}, nil
}

func supportsStreaming(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "gemini", "cloudflare":
		return false
	default:
		return true
	}
}

func cloneFloat64(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func normalizeToolsEmulationMode(mode string) uniaiapi.ToolsEmulationMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "force":
		return uniaiapi.ToolsEmulationForce
	case "fallback":
		return uniaiapi.ToolsEmulationFallback
	default:
		return uniaiapi.ToolsEmulationOff
	}
}

func shouldRetryWithoutResponseFormat(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "response_format") || strings.Contains(msg, "response format")
}

func normalizeOpenAIBase(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	endpoint = strings.TrimRight(endpoint, "/")
	if strings.Contains(endpoint, "/backend-api/codex") {
		endpoint = strings.TrimSuffix(endpoint, "/v1")
		return endpoint
	}
	// If the endpoint already ends with a version segment like /v1, /v2, /v4,
	// trust it and do not append an extra /v1.
	if matched, _ := regexp.MatchString(`/v\d+$`, endpoint); matched {
		return endpoint
	}
	if strings.HasSuffix(endpoint, "/v1") || strings.Contains(endpoint, "/v1/") {
		return endpoint
	}
	return endpoint + "/v1"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func floatFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if val, err := v.Float64(); err == nil {
			return val, true
		}
	case string:
		if val, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return val, true
		}
	}
	return 0, false
}

func intFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if val, err := v.Int64(); err == nil {
			return int(val), true
		}
	case string:
		if val, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return val, true
		}
	}
	return 0, false
}

func stringSliceFromAny(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return append([]string{}, v...), true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, false
		}
		return []string{strings.TrimSpace(v)}, true
	default:
		return nil, false
	}
}
