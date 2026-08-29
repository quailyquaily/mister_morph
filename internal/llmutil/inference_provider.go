package llmutil

import (
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/xaiauth"
)

const (
	InferenceProviderOpenAI                   = "openai"
	InferenceProviderOpenAICodex              = "openai_codex"
	InferenceProviderGemini                   = "gemini"
	InferenceProviderAnthropic                = "anthropic"
	InferenceProviderBedrock                  = "bedrock"
	InferenceProviderCloudflare               = "cloudflare"
	InferenceProviderMisterMorphPro           = "mistermorph_pro"
	InferenceProviderXAI                      = "xai"
	InferenceProviderXAIOAuth                 = "xai_oauth"
	InferenceProviderMeta                     = "meta"
	InferenceProviderDeepseek                 = "deepseek"
	InferenceProviderKimi                     = "kimi"
	InferenceProviderOpenRouter               = "openrouter"
	InferenceProviderGroq                     = "groq"
	InferenceProviderSakana                   = "sakana"
	InferenceProviderOpenAIChatCompatible     = "openai_chat_compatible"
	InferenceProviderOpenAIResponseCompatible = "openai_response_compatible"
	InferenceProviderAnthropicCompatible      = "anthropic_compatible"
)

const (
	DefaultOpenAIEndpoint         = "https://api.openai.com"
	DefaultGeminiEndpoint         = "https://generativelanguage.googleapis.com"
	DefaultAnthropicEndpoint      = "https://api.anthropic.com"
	DefaultCloudflareEndpoint     = "https://api.cloudflare.com/client/v4"
	DefaultMisterMorphProEndpoint = "https://router.mistermorph.com/api/v1"
	DefaultXAIEndpoint            = "https://api.x.ai"
	DefaultXAIOAuthEndpoint       = xaiauth.DefaultAPIBase
	DefaultMetaEndpoint           = "https://api.ai.meta.com/v1"
	DefaultDeepseekEndpoint       = "https://api.deepseek.com"
	DefaultKimiEndpoint           = "https://api.moonshot.cn"
	DefaultOpenRouterEndpoint     = "https://openrouter.ai/api/v1"
	DefaultGroqEndpoint           = "https://api.groq.com/openai/v1"
	DefaultSakanaEndpoint         = "https://api.sakana.ai/v1"
)

type InferenceProviderInfo struct {
	Label                 string
	Value                 string
	Provider              string
	Endpoint              string
	SupportsCustomAPIBase bool
	RequiresAPIBase       bool
}

var inferenceProviderRegistry = []InferenceProviderInfo{
	{Label: "OpenAI", Value: InferenceProviderOpenAI, Provider: "openai_resp", Endpoint: DefaultOpenAIEndpoint},
	{Label: "OpenAI Codex", Value: InferenceProviderOpenAICodex, Provider: "openai_codex", SupportsCustomAPIBase: true},
	{Label: "Google Gemini", Value: InferenceProviderGemini, Provider: "gemini", Endpoint: DefaultGeminiEndpoint},
	{Label: "Claude AI", Value: InferenceProviderAnthropic, Provider: "anthropic", Endpoint: DefaultAnthropicEndpoint},
	{Label: "AWS Bedrock", Value: InferenceProviderBedrock, Provider: "bedrock"},
	{Label: "Cloudflare", Value: InferenceProviderCloudflare, Provider: "cloudflare", Endpoint: DefaultCloudflareEndpoint},
	{Label: "MisterMorph Pro", Value: InferenceProviderMisterMorphPro, Provider: "openai", Endpoint: DefaultMisterMorphProEndpoint},
	{Label: "xAI", Value: InferenceProviderXAI, Provider: "xai", Endpoint: DefaultXAIEndpoint},
	{Label: "xAI Grok OAuth", Value: InferenceProviderXAIOAuth, Provider: xaiauth.ProviderName, Endpoint: DefaultXAIOAuthEndpoint},
	{Label: "Meta", Value: InferenceProviderMeta, Provider: "meta", Endpoint: DefaultMetaEndpoint},
	{Label: "Deepseek", Value: InferenceProviderDeepseek, Provider: "deepseek", Endpoint: DefaultDeepseekEndpoint},
	{Label: "Kimi", Value: InferenceProviderKimi, Provider: "openai", Endpoint: DefaultKimiEndpoint},
	{Label: "OpenRouter", Value: InferenceProviderOpenRouter, Provider: "openai", Endpoint: DefaultOpenRouterEndpoint},
	{Label: "Groq", Value: InferenceProviderGroq, Provider: "openai", Endpoint: DefaultGroqEndpoint},
	{Label: "Sakana AI", Value: InferenceProviderSakana, Provider: "sakana", Endpoint: DefaultSakanaEndpoint},
	{Label: "OpenAI Chat Compatible", Value: InferenceProviderOpenAIChatCompatible, Provider: "openai", SupportsCustomAPIBase: true, RequiresAPIBase: true},
	{Label: "OpenAI Response Compatible", Value: InferenceProviderOpenAIResponseCompatible, Provider: "openai_resp", SupportsCustomAPIBase: true, RequiresAPIBase: true},
	{Label: "Claude AI Compatible", Value: InferenceProviderAnthropicCompatible, Provider: "anthropic", SupportsCustomAPIBase: true, RequiresAPIBase: true},
}

func InferenceProviderInfoByValue(value string) (InferenceProviderInfo, bool) {
	value = normalizeInferenceProvider(value)
	for _, item := range inferenceProviderRegistry {
		if item.Value == value {
			return item, true
		}
	}
	return InferenceProviderInfo{}, false
}

func ResolveRuntimeValuesInferenceProvider(values RuntimeValues) (RuntimeValues, error) {
	inferenceProvider := normalizeInferenceProvider(values.InferenceProvider)
	if inferenceProvider == "" {
		inferenceProvider = InferInferenceProvider(values.Provider, values.Endpoint)
	}
	if inferenceProvider == "" {
		if strings.TrimSpace(values.Provider) != "" {
			return RuntimeValues{}, fmt.Errorf("invalid llm.provider %q", strings.TrimSpace(values.Provider))
		}
		return values, nil
	}
	info, ok := InferenceProviderInfoByValue(inferenceProvider)
	if !ok {
		return RuntimeValues{}, invalidInferenceProviderError(inferenceProvider)
	}
	values.InferenceProvider = info.Value
	values.Provider = info.Provider
	if info.SupportsCustomAPIBase {
		values.Endpoint = strings.TrimSpace(values.Endpoint)
	} else {
		values.Endpoint = strings.TrimSpace(info.Endpoint)
	}
	return values, nil
}

func InferInferenceProvider(provider string, endpoint string) string {
	provider = normalizeProvider(provider)
	endpoint = normalizeEndpoint(endpoint)
	switch provider {
	case "openai_codex":
		return InferenceProviderOpenAICodex
	case "gemini":
		return InferenceProviderGemini
	case "anthropic":
		if endpoint == "" || endpoint == normalizeEndpoint(DefaultAnthropicEndpoint) {
			return InferenceProviderAnthropic
		}
		return InferenceProviderAnthropicCompatible
	case "bedrock":
		return InferenceProviderBedrock
	case "cloudflare":
		return InferenceProviderCloudflare
	case "xai":
		return InferenceProviderXAI
	case xaiauth.ProviderName:
		return InferenceProviderXAIOAuth
	case "meta":
		return InferenceProviderMeta
	case "deepseek":
		return InferenceProviderDeepseek
	case "openrouter":
		return InferenceProviderOpenRouter
	case "groq":
		return InferenceProviderGroq
	case "sakana":
		return InferenceProviderSakana
	case "openai_resp":
		switch endpoint {
		case "", normalizeEndpoint(DefaultOpenAIEndpoint), normalizeEndpoint(DefaultOpenAIEndpoint + "/v1"):
			return InferenceProviderOpenAI
		case normalizeEndpoint(DefaultSakanaEndpoint):
			return InferenceProviderSakana
		default:
			return InferenceProviderOpenAIResponseCompatible
		}
	case "openai":
		switch endpoint {
		case "", normalizeEndpoint(DefaultOpenAIEndpoint), normalizeEndpoint(DefaultOpenAIEndpoint + "/v1"):
			return InferenceProviderOpenAI
		case normalizeEndpoint(DefaultMisterMorphProEndpoint):
			return InferenceProviderMisterMorphPro
		case normalizeEndpoint(DefaultXAIEndpoint), normalizeEndpoint(DefaultXAIEndpoint + "/v1"):
			return InferenceProviderXAI
		case normalizeEndpoint(DefaultMetaEndpoint):
			return InferenceProviderMeta
		case normalizeEndpoint(DefaultDeepseekEndpoint), normalizeEndpoint(DefaultDeepseekEndpoint + "/v1"):
			return InferenceProviderDeepseek
		case normalizeEndpoint(DefaultKimiEndpoint), normalizeEndpoint(DefaultKimiEndpoint + "/v1"):
			return InferenceProviderKimi
		case normalizeEndpoint(DefaultOpenRouterEndpoint):
			return InferenceProviderOpenRouter
		case normalizeEndpoint(DefaultGroqEndpoint):
			return InferenceProviderGroq
		case normalizeEndpoint(DefaultSakanaEndpoint):
			return InferenceProviderSakana
		default:
			return InferenceProviderOpenAIChatCompatible
		}
	default:
		return ""
	}
}

func normalizeInferenceProvider(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "openai_compatible":
		return InferenceProviderOpenAIChatCompatible
	case "openai_chat":
		return InferenceProviderOpenAIChatCompatible
	case "openai_response":
		return InferenceProviderOpenAIResponseCompatible
	case "claude", "claude_ai":
		return InferenceProviderAnthropic
	case "claude_compatible", "claude_ai_compatible":
		return InferenceProviderAnthropicCompatible
	case "google_gemini":
		return InferenceProviderGemini
	case "mistermorph", "mister_morph_pro":
		return InferenceProviderMisterMorphPro
	case "open_router":
		return InferenceProviderOpenRouter
	default:
		return value
	}
}

func normalizeEndpoint(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "/")
	return strings.ToLower(value)
}

func invalidInferenceProviderError(value string) error {
	allowed := make([]string, 0, len(inferenceProviderRegistry))
	for _, item := range inferenceProviderRegistry {
		allowed = append(allowed, item.Value)
	}
	return &InvalidInferenceProviderError{Value: value, Allowed: allowed}
}

type InvalidInferenceProviderError struct {
	Value   string
	Allowed []string
}

func (e *InvalidInferenceProviderError) Error() string {
	return "invalid llm.inference_provider " + strconvQuote(e.Value) + " (expected one of: " + strings.Join(e.Allowed, "|") + ")"
}

func strconvQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
