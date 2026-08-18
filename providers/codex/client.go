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
	APIKey   string
	Model    string
	Headers  map[string]string
	Pricing  *uniaiapi.PricingCatalog

	RequestTimeout     time.Duration
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
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
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
	headers := sanitizeHeaders(c.cfg.Headers)
	apiKey := c.cfg.APIKey
	if !codexauth.UsesAPIKey(c.cfg.Endpoint, apiKey) {
		token, err := codexauth.ResolveToken(ctx, c.cfg.StateDir, c.cfg.OAuth)
		if err != nil {
			return llm.Result{}, err
		}
		apiKey = token.AccessToken
		if accountID := strings.TrimSpace(token.AccountID); accountID != "" {
			headers["ChatGPT-Account-ID"] = accountID
		}
	}
	req, err := prepareCodexRequest(req)
	if err != nil {
		return llm.Result{}, err
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = c.cfg.Model
	}
	if req.OnStream == nil {
		req.OnStream = func(llm.StreamEvent) error { return nil }
	}

	base, err := uniaiProvider.New(uniaiProvider.Config{
		Provider:           "openai_codex",
		Endpoint:           c.cfg.Endpoint,
		APIKey:             apiKey,
		Model:              c.cfg.Model,
		Headers:            headers,
		Pricing:            c.cfg.Pricing,
		RequestTimeout:     c.cfg.RequestTimeout,
		ToolsEmulationMode: c.cfg.ToolsEmulationMode,
		ReasoningEffort:    c.cfg.ReasoningEffort,
	})
	if err != nil {
		return llm.Result{}, err
	}
	return base.Chat(ctx, req)
}

func prepareCodexRequest(req llm.Request) (llm.Request, error) {
	instructions, messages := splitInstructions(req.Messages)
	if instructions == "" {
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
	openAIOptions := cloneOpenAIOptions(params["openai"])
	openAIOptions["instructions"] = instructions
	openAIOptions["store"] = false
	params["openai"] = openAIOptions
	req.Parameters = params
	return req, nil
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
	if content := strings.TrimSpace(msg.Content); content != "" {
		return content
	}
	if len(msg.Parts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		if text := strings.TrimSpace(part.Text); strings.EqualFold(strings.TrimSpace(part.Type), llm.PartTypeText) && text != "" {
			parts = append(parts, text)
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
		if key == "" || strings.EqualFold(key, "authorization") || strings.EqualFold(key, "chatgpt-account-id") {
			continue
		}
		out[key] = value
	}
	return out
}
