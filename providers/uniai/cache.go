package uniai

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/lyricat/goutils/structs"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
)

func toLLMCacheControl(ctrl *uniaiapi.CacheControl) *llm.CacheControl {
	if ctrl == nil {
		return nil
	}
	return &llm.CacheControl{TTL: strings.TrimSpace(ctrl.TTL)}
}

func toUniaiCacheControlForProvider(provider, model string, ctrl llm.CacheControl) (uniaiapi.CacheControl, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if (provider == "openai" || provider == "openai_resp") && openAIModelMatchesFamily(model, "gpt-5-6") {
		return uniaiapi.CacheControl{}, true
	}
	ttl := explicitCacheTTLForProvider(provider, ctrl.TTL)
	if ttl == "" {
		return uniaiapi.CacheControl{}, false
	}
	return uniaiapi.CacheControl{TTL: ttl}, true
}

func adaptRequestForProvider(req llm.Request, provider, model, cacheTTL string) llm.Request {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic":
		return req
	case "bedrock":
		return stripExplicitCacheControl(req, true, false, true)
	case "openai", "openai_resp":
		if openAIModelMatchesFamily(model, "gpt-5-6") && !strings.EqualFold(strings.TrimSpace(cacheTTL), "off") {
			return stripExplicitCacheControl(req, false, true, true)
		}
		return stripExplicitCacheControl(req, true, true, true)
	default:
		return stripExplicitCacheControl(req, true, true, true)
	}
}

func stripExplicitCacheControl(req llm.Request, stripSystemParts, stripOtherParts, stripTools bool) llm.Request {
	out := req

	if len(req.Messages) > 0 {
		messages := make([]llm.Message, len(req.Messages))
		copy(messages, req.Messages)
		changed := false
		for i, msg := range messages {
			if len(msg.Parts) == 0 {
				continue
			}
			isSystem := strings.EqualFold(strings.TrimSpace(msg.Role), "system")
			if (isSystem && !stripSystemParts) || (!isSystem && !stripOtherParts) {
				continue
			}
			parts := make([]llm.Part, len(msg.Parts))
			copy(parts, msg.Parts)
			partChanged := false
			for j, part := range parts {
				if part.CacheControl == nil {
					continue
				}
				part.CacheControl = nil
				parts[j] = part
				partChanged = true
			}
			if partChanged {
				msg.Parts = parts
				messages[i] = msg
				changed = true
			}
		}
		if changed {
			out.Messages = messages
		}
	}

	if stripTools && len(req.Tools) > 0 {
		tools := make([]llm.Tool, len(req.Tools))
		copy(tools, req.Tools)
		changed := false
		for i, tool := range tools {
			if tool.CacheControl == nil {
				continue
			}
			tool.CacheControl = nil
			tools[i] = tool
			changed = true
		}
		if changed {
			out.Tools = tools
		}
	}

	return out
}

func applyPromptCacheOptions(provider, model, cacheTTL, cacheKeyPrefix string, req llm.Request, openAIOptions, azureOptions structs.JSONMap) {
	if strings.EqualFold(strings.TrimSpace(req.InferenceProvider), "groq") {
		return
	}
	if strings.EqualFold(strings.TrimSpace(cacheTTL), "off") {
		return
	}
	key := derivedPromptCacheKey(provider, model, cacheKeyPrefix, req)
	var target structs.JSONMap
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	switch normalizedProvider {
	case "openai", "openai_resp":
		target = openAIOptions
	case "azure":
		target = azureOptions
	default:
		return
	}
	if key != "" {
		target["prompt_cache_key"] = key
	}
	if (normalizedProvider == "openai" || normalizedProvider == "openai_resp") && openAIModelMatchesFamily(model, "gpt-5-6") {
		return
	}
	retention := promptCacheRetentionForProvider(provider, cacheTTL)
	if (normalizedProvider == "openai" || normalizedProvider == "openai_resp") && openAIModelMatchesFamily(model, "gpt-5-5") {
		retention = "24h"
	}
	if retention != "" {
		target["prompt_cache_retention"] = retention
	}
}

func openAIModelMatchesFamily(model, family string) bool {
	model = strings.ToLower(llm.ShortModelName(model))
	model = strings.ReplaceAll(model, ".", "-")
	family = strings.ToLower(strings.TrimSpace(family))
	return model == family || strings.HasPrefix(model, family+"-")
}

func promptCacheRetentionForProvider(provider, rawTTL string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "openai_resp", "azure":
	default:
		return ""
	}
	return normalizePromptCacheRetention(rawTTL)
}

func normalizePromptCacheRetention(rawTTL string) string {
	rawTTL = strings.TrimSpace(rawTTL)
	if rawTTL == "" || strings.EqualFold(rawTTL, "off") {
		return ""
	}
	switch strings.ToLower(rawTTL) {
	case "short":
		return "in_memory"
	case "long":
		return "24h"
	}
	d, err := time.ParseDuration(rawTTL)
	if err != nil {
		return ""
	}
	if d <= 5*time.Minute {
		return "in_memory"
	}
	return "24h"
}

func explicitCacheTTLForProvider(provider, rawTTL string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "bedrock":
	default:
		return ""
	}
	rawTTL = strings.TrimSpace(rawTTL)
	if rawTTL == "" || strings.EqualFold(rawTTL, "off") {
		return ""
	}
	switch strings.ToLower(rawTTL) {
	case "short":
		return "5m"
	case "long":
		return "1h"
	}
	d, err := time.ParseDuration(rawTTL)
	if err != nil {
		return ""
	}
	if d <= 5*time.Minute {
		return "5m"
	}
	return "1h"
}

func derivedPromptCacheKey(provider, model, cacheKeyPrefix string, req llm.Request) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "openai_resp", "azure":
	default:
		return ""
	}
	cacheKeyPrefix = strings.TrimSpace(cacheKeyPrefix)

	stable := promptCacheStablePayload{
		Model: strings.TrimSpace(model),
		Scene: strings.TrimSpace(req.Scene),
	}
	for _, msg := range req.Messages {
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "system") {
			continue
		}
		stable.Messages = append(stable.Messages, stablePromptMessage{
			Content: strings.TrimSpace(msg.Content),
			Parts:   stableParts(msg.Parts),
		})
	}
	for _, tool := range req.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		stable.Tools = append(stable.Tools, stablePromptTool{
			Name:           name,
			Description:    strings.TrimSpace(tool.Description),
			ParametersJSON: strings.TrimSpace(tool.ParametersJSON),
		})
	}
	if len(stable.Messages) == 0 && len(stable.Tools) == 0 {
		return cacheKeyPrefix
	}
	data, err := json.Marshal(stable)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	key := "mm-" + base64.RawURLEncoding.EncodeToString(sum[:12])
	if cacheKeyPrefix == "" {
		return key
	}
	return cacheKeyPrefix + "-" + key
}

type promptCacheStablePayload struct {
	Model    string                `json:"model,omitempty"`
	Scene    string                `json:"scene,omitempty"`
	Messages []stablePromptMessage `json:"messages,omitempty"`
	Tools    []stablePromptTool    `json:"tools,omitempty"`
}

type stablePromptMessage struct {
	Content string       `json:"content,omitempty"`
	Parts   []stablePart `json:"parts,omitempty"`
}

type stablePromptTool struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	ParametersJSON string `json:"parameters_json,omitempty"`
}

type stablePart struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	URL        string `json:"url,omitempty"`
	DataBase64 string `json:"data_base64,omitempty"`
	MIMEType   string `json:"mime_type,omitempty"`
}

func stableParts(parts []llm.Part) []stablePart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]stablePart, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type)
		if partType == "" {
			continue
		}
		out = append(out, stablePart{
			Type:       partType,
			Text:       strings.TrimSpace(part.Text),
			URL:        strings.TrimSpace(part.URL),
			DataBase64: strings.TrimSpace(part.DataBase64),
			MIMEType:   strings.TrimSpace(part.MIMEType),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
