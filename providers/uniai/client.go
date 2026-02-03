package uniai

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/goutils/structs"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
	uniaichat "github.com/quailyquaily/uniai/chat"
)

type Config struct {
	Provider string
	Endpoint string
	APIKey   string
	Model    string

	RequestTimeout time.Duration

	ToolsEmulation     bool
	AzureAPIKey        string
	AzureEndpoint      string
	AzureDeployment    string
	AwsKey             string
	AwsSecret          string
	AwsRegion          string
	AwsBedrockModelArn string
}

type Client struct {
	provider       string
	requestTimeout time.Duration
	toolsEmulation bool
	client         *uniaiapi.Client
}

func New(cfg Config) *Client {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))

	openAIBase := normalizeOpenAIBase(cfg.Endpoint)
	openAIKey := strings.TrimSpace(cfg.APIKey)

	azureAPIKey := firstNonEmpty(cfg.AzureAPIKey, cfg.APIKey)
	azureEndpoint := firstNonEmpty(cfg.AzureEndpoint, cfg.Endpoint)
	azureDeployment := firstNonEmpty(cfg.AzureDeployment, cfg.Model)

	anthropicKey := strings.TrimSpace(cfg.APIKey)
	anthropicModel := strings.TrimSpace(cfg.Model)

	geminiKey := strings.TrimSpace(cfg.APIKey)
	geminiBase := strings.TrimSpace(cfg.Endpoint)

	susanooKey := strings.TrimSpace(cfg.APIKey)
	susanooBase := strings.TrimSpace(cfg.Endpoint)

	uCfg := uniaiapi.Config{
		Provider:            provider,
		OpenAIAPIKey:        openAIKey,
		OpenAIAPIBase:       openAIBase,
		OpenAIModel:         strings.TrimSpace(cfg.Model),
		AzureOpenAIAPIKey:   strings.TrimSpace(azureAPIKey),
		AzureOpenAIEndpoint: strings.TrimSpace(azureEndpoint),
		AzureOpenAIModel:    strings.TrimSpace(azureDeployment),
		AnthropicAPIKey:     strings.TrimSpace(anthropicKey),
		AnthropicModel:      strings.TrimSpace(anthropicModel),
		AwsKey:              strings.TrimSpace(cfg.AwsKey),
		AwsSecret:           strings.TrimSpace(cfg.AwsSecret),
		AwsRegion:           strings.TrimSpace(cfg.AwsRegion),
		AwsBedrockModelArn:  strings.TrimSpace(cfg.AwsBedrockModelArn),
		SusanooAPIBase:      strings.TrimSpace(susanooBase),
		SusanooAPIKey:       strings.TrimSpace(susanooKey),
		GeminiAPIKey:        strings.TrimSpace(geminiKey),
		GeminiAPIBase:       strings.TrimSpace(geminiBase),
	}

	return &Client{
		provider:       provider,
		requestTimeout: cfg.RequestTimeout,
		toolsEmulation: cfg.ToolsEmulation,
		client:         uniaiapi.New(uCfg),
	}
}

func (c *Client) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	start := time.Now()
	if c.requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		defer cancel()
	}

	opts := buildChatOptions(req, c.provider, true, c.toolsEmulation)
	resp, err := c.client.Chat(ctx, opts...)
	if err != nil && req.ForceJSON && shouldRetryWithoutResponseFormat(err) {
		opts = buildChatOptions(req, c.provider, false, c.toolsEmulation)
		resp, err = c.client.Chat(ctx, opts...)
	}
	if err != nil {
		return llm.Result{}, err
	}
	if resp == nil {
		return llm.Result{}, fmt.Errorf("uniai: empty response")
	}

	toolCalls := toLLMToolCalls(resp.ToolCalls)

	return llm.Result{
		Text:      resp.Text,
		ToolCalls: toolCalls,
		Usage: llm.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
		Duration: time.Since(start),
	}, nil
}

func buildChatOptions(req llm.Request, provider string, forceJSON bool, toolsEmulation bool) []uniaiapi.ChatOption {
	msgs := make([]uniaiapi.Message, len(req.Messages))
	for i, m := range req.Messages {
		msg := uniaiapi.Message{Role: m.Role, Content: m.Content}
		if strings.TrimSpace(m.ToolCallID) != "" {
			msg.ToolCallID = strings.TrimSpace(m.ToolCallID)
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = toUniaiToolCallsFromLLM(m.ToolCalls)
		}
		msgs[i] = msg
	}

	opts := []uniaiapi.ChatOption{uniaiapi.WithMessages(msgs...)}
	if provider != "" {
		opts = append(opts, uniaiapi.WithProvider(provider))
	}
	if strings.TrimSpace(req.Model) != "" {
		opts = append(opts, uniaiapi.WithModel(strings.TrimSpace(req.Model)))
	}

	if len(req.Tools) > 0 {
		tools := make([]uniaiapi.Tool, 0, len(req.Tools))
		for _, t := range req.Tools {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				continue
			}
			tools = append(tools, uniaiapi.FunctionTool(
				name,
				strings.TrimSpace(t.Description),
				[]byte(t.ParametersJSON),
			))
		}
		if len(tools) > 0 {
			opts = append(opts, uniaiapi.WithTools(tools))
			opts = append(opts, uniaiapi.WithToolChoice(uniaiapi.ToolChoiceAuto()))
			if toolsEmulation {
				opts = append(opts, uniaiapi.WithToolsEmulation(true))
			}
		}
	}

	appliedTemperature := false
	if req.Parameters != nil {
		if v, ok := floatFromAny(req.Parameters["temperature"]); ok {
			opts = append(opts, uniaiapi.WithTemperature(v))
			appliedTemperature = true
		}
		if v, ok := floatFromAny(req.Parameters["top_p"]); ok {
			opts = append(opts, uniaiapi.WithTopP(v))
		}
		if v, ok := intFromAny(req.Parameters["max_tokens"]); ok && v > 0 {
			opts = append(opts, uniaiapi.WithMaxTokens(v))
		}
		if v, ok := stringSliceFromAny(req.Parameters["stop"]); ok && len(v) > 0 {
			opts = append(opts, uniaiapi.WithStopWords(v...))
		}
		if v, ok := floatFromAny(req.Parameters["presence_penalty"]); ok {
			opts = append(opts, uniaiapi.WithPresencePenalty(v))
		}
		if v, ok := floatFromAny(req.Parameters["frequency_penalty"]); ok {
			opts = append(opts, uniaiapi.WithFrequencyPenalty(v))
		}
		if v, ok := req.Parameters["user"].(string); ok && strings.TrimSpace(v) != "" {
			opts = append(opts, uniaiapi.WithUser(strings.TrimSpace(v)))
		}
	}
	if !appliedTemperature {
		opts = append(opts, uniaiapi.WithTemperature(0))
	}

	if forceJSON {
		opts = append(opts, uniaichat.WithOpenAIOptions(structs.JSONMap{
			"response_format": "json_object",
		}))
	}

	return opts
}

func toLLMToolCalls(calls []uniaiapi.ToolCall) []llm.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]llm.ToolCall, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}
		params := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &params); err != nil {
				params = map[string]any{"_raw": call.Function.Arguments}
			}
		}
		out = append(out, llm.ToolCall{
			ID:        call.ID,
			Name:      name,
			Arguments: params,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toUniaiToolCallsFromLLM(calls []llm.ToolCall) []uniaiapi.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]uniaiapi.ToolCall, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		args := "{}"
		if call.Arguments != nil {
			if data, err := json.Marshal(call.Arguments); err == nil {
				args = string(data)
			}
		}
		out = append(out, uniaiapi.ToolCall{
			ID:   strings.TrimSpace(call.ID),
			Type: "function",
			Function: uniaiapi.ToolCallFunction{
				Name:      name,
				Arguments: args,
			},
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
