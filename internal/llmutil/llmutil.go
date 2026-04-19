package llmutil

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/pricingutil"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiProvider "github.com/quailyquaily/mistermorph/providers/uniai"
	uniaiapi "github.com/quailyquaily/uniai"
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
	Headers            map[string]string
	CacheTTL           string `config:"llm.cache_ttl"`
	AzureDeployment    string `config:"llm.azure.deployment"`
	RequestTimeoutRaw  string `config:"llm.request_timeout"`
	ToolsEmulationMode string `config:"llm.tools_emulation_mode"`
	TemperatureRaw     string `config:"llm.temperature"`
	ReasoningEffortRaw string `config:"llm.reasoning_effort"`
	ReasoningBudgetRaw string `config:"llm.reasoning_budget_tokens"`
	PricingFile        string `config:"llm.pricing_file"`
	ConfigPath         string `config:"config"`
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
		Provider:           strings.TrimSpace(r.GetString("llm.provider")),
		Endpoint:           strings.TrimSpace(r.GetString("llm.endpoint")),
		APIKey:             strings.TrimSpace(r.GetString("llm.api_key")),
		Model:              strings.TrimSpace(r.GetString("llm.model")),
		Headers:            loadStringMapKeyFromReader(r, "llm.headers"),
		CacheTTL:           strings.TrimSpace(r.GetString("llm.cache_ttl")),
		AzureDeployment:    strings.TrimSpace(r.GetString("llm.azure.deployment")),
		RequestTimeoutRaw:  strings.TrimSpace(r.GetString("llm.request_timeout")),
		ToolsEmulationMode: strings.TrimSpace(r.GetString("llm.tools_emulation_mode")),
		TemperatureRaw:     strings.TrimSpace(r.GetString("llm.temperature")),
		ReasoningEffortRaw: strings.TrimSpace(r.GetString("llm.reasoning_effort")),
		ReasoningBudgetRaw: strings.TrimSpace(r.GetString("llm.reasoning_budget_tokens")),
		PricingFile:        strings.TrimSpace(r.GetString("llm.pricing_file")),
		ConfigPath:         strings.TrimSpace(r.GetString("config")),
		Profiles:           loadLLMProfilesFromReader(r),
		Routes:             loadLLMRoutesFromReader(r),
		BedrockAWSKey:      firstNonEmpty(r.GetString("llm.bedrock.aws_key"), r.GetString("llm.aws.key")),
		BedrockAWSSecret:   firstNonEmpty(r.GetString("llm.bedrock.aws_secret"), r.GetString("llm.aws.secret")),
		BedrockAWSRegion:   firstNonEmpty(r.GetString("llm.bedrock.region"), r.GetString("llm.aws.region")),
		BedrockModelARN:    firstNonEmpty(r.GetString("llm.bedrock.model_arn"), r.GetString("llm.aws.bedrock_model_arn")),
		CloudflareAccountID: firstNonEmpty(
			r.GetString("llm.cloudflare.account_id"),
		),
		CloudflareAPIToken: firstNonEmpty(
			r.GetString("llm.cloudflare.api_token"),
		),
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
	pricing, _, err := LoadPricingCatalog(values)
	if err != nil {
		return nil, err
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "openai_resp" && reasoningBudget != nil {
		slog.Warn("llm_reasoning_budget_ignored", "provider", provider, "field", "llm.reasoning_budget_tokens")
	}
	// uniaiapi doesn't recognize "openai_custom" as a provider name;
	// it's just an OpenAI-compatible endpoint with a custom base URL.
	// Map it to "openai" for the uniai provider while preserving the
	// original name for any provider-specific logic elsewhere.
	uniaiProviderName := provider
	if provider == "openai_custom" {
		uniaiProviderName = "openai"
	}
	switch provider {
	case "openai", "openai_resp", "openai_custom", "deepseek", "xai", "gemini", "azure", "anthropic", "bedrock", "susanoo", "cloudflare":
		c := uniaiProvider.New(uniaiProvider.Config{
			Provider:           uniaiProviderName,
			Endpoint:           strings.TrimSpace(cfg.Endpoint),
			APIKey:             strings.TrimSpace(cfg.APIKey),
			Model:              strings.TrimSpace(cfg.Model),
			Headers:            cloneStringMap(cfg.Headers),
			Pricing:            pricing,
			RequestTimeout:     cfg.RequestTimeout,
			CacheTTL:           strings.TrimSpace(values.CacheTTL),
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

func LoadPricingCatalog(values RuntimeValues) (*uniaiapi.PricingCatalog, string, error) {
	return pricingutil.LoadCatalog(values.PricingFile, values.ConfigPath)
}

type BaseClientBuilder func(cfg llmconfig.ClientConfig, values RuntimeValues) (llm.Client, error)

type ClientWrapFunc func(client llm.Client, cfg llmconfig.ClientConfig, profile string) llm.Client

func BuildRouteClient(route ResolvedRoute, primaryOverride *llmconfig.ClientConfig, build BaseClientBuilder, wrap ClientWrapFunc, logger *slog.Logger) (llm.Client, error) {
	if build == nil {
		return nil, fmt.Errorf("base client builder is nil")
	}
	if len(route.Candidates) > 0 {
		return buildWeightedRouteClient(route, primaryOverride, build, wrap, logger)
	}
	primaryCfg := route.ClientConfig
	if primaryOverride != nil {
		primaryCfg = *primaryOverride
	}
	primaryClient, err := build(primaryCfg, route.Values)
	if err != nil {
		return nil, err
	}
	if wrap != nil {
		primaryClient = wrap(primaryClient, primaryCfg, route.Profile)
	}
	if len(route.Fallbacks) == 0 {
		return primaryClient, nil
	}

	candidates := make([]FallbackCandidate, 0, len(route.Fallbacks))
	for _, fallback := range route.Fallbacks {
		client, err := build(fallback.ClientConfig, fallback.Values)
		if err != nil {
			return nil, err
		}
		if wrap != nil {
			client = wrap(client, fallback.ClientConfig, fallback.Profile)
		}
		candidates = append(candidates, FallbackCandidate{
			Profile: fallback.Profile,
			Model:   strings.TrimSpace(fallback.ClientConfig.Model),
			Client:  client,
		})
	}

	return NewFallbackClient(FallbackClientOptions{
		Primary:        primaryClient,
		PrimaryProfile: route.Profile,
		PrimaryModel:   strings.TrimSpace(primaryCfg.Model),
		Fallbacks:      candidates,
		Logger:         logger,
	}), nil
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

func SystemPromptCacheControl(rawTTL string) (*llm.CacheControl, error) {
	rawTTL = strings.TrimSpace(rawTTL)
	if rawTTL == "" || strings.EqualFold(rawTTL, "off") {
		return nil, nil
	}

	switch strings.ToLower(rawTTL) {
	case "short", "long":
		return &llm.CacheControl{TTL: strings.ToLower(rawTTL)}, nil
	}

	if _, err := time.ParseDuration(rawTTL); err != nil {
		return nil, fmt.Errorf("invalid llm.cache_ttl %q (expected off|short|long|Go duration)", rawTTL)
	}
	return &llm.CacheControl{TTL: rawTTL}, nil
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

func loadStringSliceKeyFromReader(r ConfigReader, key string) []string {
	if getter, ok := any(r).(interface{ GetStringSlice(string) []string }); ok {
		return normalizeProfileNames(getter.GetStringSlice(key))
	}
	var raw []string
	if err := unmarshalKey(r, key, &raw); err == nil {
		return normalizeProfileNames(raw)
	}
	return nil
}

func loadStringMapKeyFromReader(r ConfigReader, key string) map[string]string {
	var raw map[string]string
	if err := unmarshalKey(r, key, &raw); err == nil {
		return cloneStringMap(raw)
	}
	if getter, ok := any(r).(interface {
		GetStringMapString(string) map[string]string
	}); ok {
		return cloneStringMap(getter.GetStringMapString(key))
	}
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		out[k] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
