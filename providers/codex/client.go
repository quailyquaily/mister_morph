package codex

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lyricat/goutils/structs"
	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiProvider "github.com/quailyquaily/mistermorph/providers/uniai"
	uniaiapi "github.com/quailyquaily/uniai"
)

type Config struct {
	Endpoint string
	Model    string
	Headers  map[string]string
	Pricing  *uniaiapi.PricingCatalog

	RequestTimeout     time.Duration
	Temperature        *float64
	ReasoningEffort    string
	ToolsEmulationMode string
	StateDir           string
	OAuth              codexauth.OAuthConfig
}

type Client struct {
	cfg Config
}

const codexInstructionsMaxBytes = 30 * 1024

func New(cfg Config) *Client {
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if cfg.Endpoint == "" {
		cfg.Endpoint = codexauth.DefaultAPIBase
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Model == "" {
		cfg.Model = codexauth.DefaultModel
	}
	cfg.Headers = sanitizeHeaders(cfg.Headers)
	cfg.ReasoningEffort = strings.TrimSpace(cfg.ReasoningEffort)
	cfg.ToolsEmulationMode = strings.TrimSpace(cfg.ToolsEmulationMode)
	return &Client{cfg: cfg}
}

func (c *Client) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	if c == nil {
		return llm.Result{}, fmt.Errorf("codex provider is nil")
	}
	token, err := codexauth.ResolveToken(ctx, c.cfg.StateDir, c.cfg.OAuth)
	if err != nil {
		return llm.Result{}, err
	}
	req, err = prepareCodexRequest(req)
	if err != nil {
		return llm.Result{}, err
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = c.cfg.Model
	}
	if req.OnStream == nil {
		req.OnStream = func(llm.StreamEvent) error { return nil }
	}

	headers := sanitizeHeaders(c.cfg.Headers)
	if accountID := strings.TrimSpace(token.AccountID); accountID != "" {
		headers["ChatGPT-Account-ID"] = accountID
	}

	base, err := uniaiProvider.New(uniaiProvider.Config{
		Provider:           "openai_resp",
		Endpoint:           c.cfg.Endpoint,
		APIKey:             token.AccessToken,
		Model:              c.cfg.Model,
		Headers:            headers,
		Pricing:            c.cfg.Pricing,
		RequestTimeout:     c.cfg.RequestTimeout,
		CacheTTL:           "off",
		ToolsEmulationMode: c.cfg.ToolsEmulationMode,
		Temperature:        c.cfg.Temperature,
		ReasoningEffort:    c.cfg.ReasoningEffort,
	})
	if err != nil {
		return llm.Result{}, err
	}
	result, err := base.Chat(ctx, req)
	if err != nil {
		return llm.Result{}, err
	}
	return result, nil
}

func prepareCodexRequest(req llm.Request) (llm.Request, error) {
	instructions, messages := splitInstructions(req.Messages)
	if strings.TrimSpace(instructions) == "" {
		return llm.Request{}, fmt.Errorf("openai_codex requires at least one system or developer message")
	}
	instructions, overflow := splitInstructionLimit(instructions, codexInstructionsMaxBytes)
	if overflow != "" {
		messages = append([]llm.Message{{
			Role:    "system",
			Content: "Additional system and developer instructions:\n\n" + overflow,
		}}, messages...)
	}
	req.Messages = messages
	params := cloneAnyMap(req.Parameters)
	delete(params, "max_tokens")
	openAIOptions := cloneOpenAIOptions(params["openai"])
	delete(openAIOptions, "max_tokens")
	delete(openAIOptions, "max_output_tokens")
	openAIOptions["instructions"] = instructions
	openAIOptions["store"] = false
	if strings.TrimSpace(openAIOptions.GetString("prompt_cache_key")) == "" {
		openAIOptions["prompt_cache_key"] = "mistermorph"
	}
	if req.ForceJSON && strings.TrimSpace(openAIOptions.GetString("response_format")) == "" {
		openAIOptions["response_format"] = "json_object"
	}
	if usesJSONResponseFormat(openAIOptions["response_format"]) {
		req.Messages = ensureInputMentionsJSON(req.Messages)
	}
	params["openai"] = openAIOptions
	req.Parameters = params
	return req, nil
}

func usesJSONResponseFormat(value any) bool {
	switch v := value.(type) {
	case string:
		return responseFormatTypeRequiresJSON(v)
	case structs.JSONMap:
		return responseFormatMapRequiresJSON(v)
	case map[string]any:
		return responseFormatMapRequiresJSON(v)
	default:
		return false
	}
}

func responseFormatMapRequiresJSON(value map[string]any) bool {
	rawType, ok := value["type"]
	if !ok {
		return false
	}
	typeName, ok := rawType.(string)
	return ok && responseFormatTypeRequiresJSON(typeName)
}

func responseFormatTypeRequiresJSON(typeName string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(typeName)), "json")
}

func ensureInputMentionsJSON(messages []llm.Message) []llm.Message {
	if inputMentionsJSON(messages) {
		return messages
	}
	out := make([]llm.Message, 0, len(messages)+1)
	out = append(out, llm.Message{
		Role:    "user",
		Content: "JSON response format reminder: return a JSON object as instructed.",
	})
	out = append(out, messages...)
	return out
}

func inputMentionsJSON(messages []llm.Message) bool {
	for _, msg := range messages {
		if strings.Contains(strings.ToLower(messageText(msg)), "json") {
			return true
		}
	}
	return false
}

func splitInstructionLimit(text string, maxBytes int) (string, string) {
	text = strings.TrimSpace(text)
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text, ""
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	if cut <= 0 {
		return text, ""
	}
	return strings.TrimSpace(text[:cut]), strings.TrimSpace(text[cut:])
}

func splitInstructions(messages []llm.Message) (string, []llm.Message) {
	if len(messages) == 0 {
		return "", nil
	}
	instructions := make([]string, 0, 2)
	remaining := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role != "system" && role != "developer" {
			remaining = append(remaining, msg)
			continue
		}
		text := strings.TrimSpace(messageText(msg))
		if text != "" {
			instructions = append(instructions, text)
		}
	}
	return strings.Join(instructions, "\n\n"), remaining
}

func messageText(msg llm.Message) string {
	if strings.TrimSpace(msg.Content) != "" {
		return strings.TrimSpace(msg.Content)
	}
	if len(msg.Parts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		if strings.EqualFold(strings.TrimSpace(part.Type), llm.PartTypeText) && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneOpenAIOptions(raw any) structs.JSONMap {
	out := structs.JSONMap{}
	switch v := raw.(type) {
	case nil:
		return out
	case structs.JSONMap:
		for key, value := range v {
			out[key] = value
		}
	case map[string]any:
		for key, value := range v {
			out[key] = value
		}
	}
	return out
}

func sanitizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" || strings.EqualFold(key, "authorization") {
			continue
		}
		out[key] = value
	}
	return out
}
