package llmutil

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiProvider "github.com/quailyquaily/mistermorph/providers/uniai"
	"github.com/spf13/viper"
)

type ConfigReader interface {
	GetString(string) string
}

type RuntimeValues struct {
	Provider           string `config:"llm.provider"`
	Endpoint           string `config:"llm.endpoint"`
	APIKey             string `config:"llm.api_key"`
	Model              string `config:"llm.model"`
	AzureDeployment    string `config:"llm.azure.deployment"`
	RequestTimeoutRaw  string `config:"llm.request_timeout"`
	ToolsEmulationMode string `config:"llm.tools_emulation_mode"`
	TemperatureRaw     string `config:"llm.temperature"`
	ReasoningEffortRaw string `config:"llm.reasoning_effort"`
	ReasoningBudgetRaw string `config:"llm.reasoning_budget_tokens"`
	Profiles           map[string]ProfileConfig
	Routes             RoutesConfig

	BedrockAWSKey       string `config:"llm.bedrock.aws_key"`
	BedrockAWSSecret    string `config:"llm.bedrock.aws_secret"`
	BedrockAWSRegion    string `config:"llm.bedrock.region"`
	BedrockModelARN     string `config:"llm.bedrock.model_arn"`
	CloudflareAccountID string `config:"llm.cloudflare.account_id"`
	CloudflareAPIToken  string `config:"llm.cloudflare.api_token"`
}

func RuntimeValuesFromReader(r ConfigReader) RuntimeValues {
	if r == nil {
		return RuntimeValues{}
	}
	return RuntimeValues{
		Provider:            strings.TrimSpace(r.GetString("llm.provider")),
		Endpoint:            strings.TrimSpace(r.GetString("llm.endpoint")),
		APIKey:              strings.TrimSpace(r.GetString("llm.api_key")),
		Model:               strings.TrimSpace(r.GetString("llm.model")),
		AzureDeployment:     strings.TrimSpace(r.GetString("llm.azure.deployment")),
		RequestTimeoutRaw:   strings.TrimSpace(r.GetString("llm.request_timeout")),
		ToolsEmulationMode:  strings.TrimSpace(r.GetString("llm.tools_emulation_mode")),
		TemperatureRaw:      strings.TrimSpace(r.GetString("llm.temperature")),
		ReasoningEffortRaw:  strings.TrimSpace(r.GetString("llm.reasoning_effort")),
		ReasoningBudgetRaw:  strings.TrimSpace(r.GetString("llm.reasoning_budget_tokens")),
		Profiles:            loadLLMProfilesFromReader(r),
		Routes:              loadLLMRoutesFromReader(r),
		BedrockAWSKey:       firstNonEmpty(r.GetString("llm.bedrock.aws_key"), r.GetString("llm.aws.key")),
		BedrockAWSSecret:    firstNonEmpty(r.GetString("llm.bedrock.aws_secret"), r.GetString("llm.aws.secret")),
		BedrockAWSRegion:    firstNonEmpty(r.GetString("llm.bedrock.region"), r.GetString("llm.aws.region")),
		BedrockModelARN:     firstNonEmpty(r.GetString("llm.bedrock.model_arn"), r.GetString("llm.aws.bedrock_model_arn")),
		CloudflareAccountID: firstNonEmpty(r.GetString("llm.cloudflare.account_id")),
		CloudflareAPIToken:  firstNonEmpty(r.GetString("llm.cloudflare.api_token")),
	}
}

func RuntimeValuesFromViper() RuntimeValues {
	return RuntimeValuesFromReader(viper.GetViper())
}

func ModelFromViper() string {
	values := RuntimeValuesFromViper()
	return ModelForProviderWithValues(values.Provider, values)
}

func EndpointForProviderWithValues(provider string, values RuntimeValues) string {
	provider = normalizeProvider(provider)
	switch provider {
	case "cloudflare":
		generic := strings.TrimSpace(values.Endpoint)
		if generic != "" && generic != "https://api.openai.com" && generic != "https://api.openai.com/v1" {
			return generic
		}
		return ""
	default:
		return strings.TrimSpace(values.Endpoint)
	}
}

func APIKeyForProviderWithValues(provider string, values RuntimeValues) string {
	provider = normalizeProvider(provider)
	switch provider {
	case "cloudflare":
		return firstNonEmpty(
			values.CloudflareAPIToken,
			values.APIKey,
		)
	default:
		return strings.TrimSpace(values.APIKey)
	}
}

func ModelForProviderWithValues(provider string, values RuntimeValues) string {
	provider = normalizeProvider(provider)
	switch provider {
	case "azure":
		return firstNonEmpty(
			values.AzureDeployment,
			values.Model,
		)
	default:
		return strings.TrimSpace(values.Model)
	}
}

func ClientFromConfigWithValues(cfg llmconfig.ClientConfig, values RuntimeValues) (llm.Client, error) {
	toolsEmulationMode, err := toolsEmulationModeFromValue(values.ToolsEmulationMode)
	if err != nil {
		return nil, err
	}
	temperature, err := optionalFloat64FromValue(values.TemperatureRaw, "llm.temperature")
	if err != nil {
		return nil, err
	}
	reasoningEffort, err := reasoningEffortFromValue(values.ReasoningEffortRaw)
	if err != nil {
		return nil, err
	}
	reasoningBudget, err := optionalIntFromValue(values.ReasoningBudgetRaw, "llm.reasoning_budget_tokens")
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "openai", "openai_custom", "deepseek", "xai", "gemini", "azure", "anthropic", "bedrock", "susanoo", "cloudflare":
		c := uniaiProvider.New(uniaiProvider.Config{
			Provider:           strings.ToLower(strings.TrimSpace(cfg.Provider)),
			Endpoint:           strings.TrimSpace(cfg.Endpoint),
			APIKey:             strings.TrimSpace(cfg.APIKey),
			Model:              strings.TrimSpace(cfg.Model),
			RequestTimeout:     cfg.RequestTimeout,
			ToolsEmulationMode: toolsEmulationMode,
			Temperature:        temperature,
			ReasoningEffort:    reasoningEffort,
			ReasoningBudget:    reasoningBudget,
			AzureAPIKey:        strings.TrimSpace(cfg.APIKey),
			AzureEndpoint:      strings.TrimSpace(cfg.Endpoint),
			AzureDeployment:    strings.TrimSpace(cfg.Model),
			AwsKey:             firstNonEmpty(values.BedrockAWSKey),
			AwsSecret:          firstNonEmpty(values.BedrockAWSSecret),
			AwsRegion:          firstNonEmpty(values.BedrockAWSRegion),
			AwsBedrockModelArn: firstNonEmpty(values.BedrockModelARN),
			CloudflareAccountID: firstNonEmpty(
				values.CloudflareAccountID,
			),
			CloudflareAPIToken: firstNonEmpty(
				values.CloudflareAPIToken,
				values.APIKey,
			),
			CloudflareAPIBase: strings.TrimSpace(cfg.Endpoint),
		})
		return c, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

func toolsEmulationModeFromValue(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return "off", nil
	}
	switch mode {
	case "off", "fallback", "force":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid llm.tools_emulation_mode %q (expected off|fallback|force)", mode)
	}
}

func optionalFloat64FromValue(raw, path string) (*float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid %s %q", path, raw)
	}
	return &v, nil
}

func optionalIntFromValue(raw, path string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s %q", path, raw)
	}
	return &v, nil
}

func reasoningEffortFromValue(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", nil
	}
	switch value {
	case "none", "minimal", "low", "medium", "high", "max", "xhigh":
		return value, nil
	default:
		return "", fmt.Errorf("invalid llm.reasoning_effort %q (expected none|minimal|low|medium|high|max|xhigh)", raw)
	}
}

func requestTimeoutFromValue(raw, path string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", path, raw)
	}
	return value, nil
}

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "openai"
	}
	return provider
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
