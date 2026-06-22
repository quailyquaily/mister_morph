package llmutil

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/pricingutil"
	"github.com/quailyquaily/mistermorph/internal/proaccount"
	"github.com/quailyquaily/mistermorph/llm"
	codexProvider "github.com/quailyquaily/mistermorph/providers/codex"
	uniaiProvider "github.com/quailyquaily/mistermorph/providers/uniai"
	uniaiapi "github.com/quailyquaily/uniai"
	"github.com/spf13/viper"
)

type ConfigReader interface {
	GetString(string) string
}

type RuntimeValues struct {
	InferenceProvider  string `config:"llm.inference_provider"`
	Provider           string `config:"llm.provider"`
	Endpoint           string `config:"llm.endpoint"`
	APIKey             string `config:"llm.api_key"`
	Model              string `config:"llm.model"`
	ContextWindowRaw   string `config:"llm.context_window_tokens"`
	Headers            map[string]string
	CacheTTL           string `config:"llm.cache_ttl"`
	CacheKeyPrefix     string `config:"llm.cache_key_prefix"`
	AzureDeployment    string `config:"llm.azure.deployment"`
	RequestTimeoutRaw  string `config:"llm.request_timeout"`
	ToolsEmulationMode string `config:"llm.tools_emulation_mode"`
	TemperatureRaw     string `config:"llm.temperature"`
	ReasoningEffortRaw string `config:"llm.reasoning_effort"`
	ReasoningBudgetRaw string `config:"llm.reasoning_budget_tokens"`
	PricingFile        string `config:"llm.pricing_file"`
	ConfigPath         string `config:"config"`
	FileStateDir       string `config:"file_state_dir"`
	Profiles           map[string]ProfileConfig
	Routes             RoutesConfig
	ImageProvider      string
	ImageEndpoint      string
	ImageAPIKey        string
	ImageModel         string
	ImageTimeoutRaw    string
	ImageOptions       llm.ImageProviderOptions

	BedrockAWSKey          string `config:"llm.bedrock.aws_key"`
	BedrockAWSSecret       string `config:"llm.bedrock.aws_secret"`
	BedrockAWSSessionToken string `config:"llm.bedrock.aws_session_token"`
	BedrockAWSProfile      string `config:"llm.bedrock.aws_profile"`
	BedrockAWSRegion       string `config:"llm.bedrock.region"`
	BedrockModelARN        string `config:"llm.bedrock.model_arn"`
	CloudflareAccountID    string `config:"llm.cloudflare.account_id"`
	CloudflareAPIToken     string `config:"llm.cloudflare.api_token"`
}

type imageClientMetadata struct {
	Provider string
	Endpoint string
	Model    string
}

func RuntimeValuesFromReader(r ConfigReader) RuntimeValues {
	if r == nil {
		return RuntimeValues{}
	}
	return RuntimeValues{
		InferenceProvider:  strings.TrimSpace(r.GetString("llm.inference_provider")),
		Provider:           strings.TrimSpace(r.GetString("llm.provider")),
		Endpoint:           strings.TrimSpace(r.GetString("llm.endpoint")),
		APIKey:             strings.TrimSpace(r.GetString("llm.api_key")),
		Model:              strings.TrimSpace(r.GetString("llm.model")),
		ContextWindowRaw:   strings.TrimSpace(r.GetString("llm.context_window_tokens")),
		Headers:            loadStringMapKeyFromReader(r, "llm.headers"),
		CacheTTL:           strings.TrimSpace(r.GetString("llm.cache_ttl")),
		CacheKeyPrefix:     strings.TrimSpace(r.GetString("llm.cache_key_prefix")),
		AzureDeployment:    strings.TrimSpace(r.GetString("llm.azure.deployment")),
		RequestTimeoutRaw:  strings.TrimSpace(r.GetString("llm.request_timeout")),
		ToolsEmulationMode: strings.TrimSpace(r.GetString("llm.tools_emulation_mode")),
		TemperatureRaw:     strings.TrimSpace(r.GetString("llm.temperature")),
		ReasoningEffortRaw: strings.TrimSpace(r.GetString("llm.reasoning_effort")),
		ReasoningBudgetRaw: strings.TrimSpace(r.GetString("llm.reasoning_budget_tokens")),
		PricingFile:        strings.TrimSpace(r.GetString("llm.pricing_file")),
		ConfigPath:         strings.TrimSpace(r.GetString("config")),
		FileStateDir:       strings.TrimSpace(r.GetString("file_state_dir")),
		Profiles:           loadLLMProfilesFromReader(r),
		Routes:             loadLLMRoutesFromReader(r),
		ImageProvider:      strings.TrimSpace(r.GetString("llm.image.provider")),
		ImageEndpoint:      strings.TrimSpace(r.GetString("llm.image.endpoint")),
		ImageAPIKey:        strings.TrimSpace(r.GetString("llm.image.api_key")),
		ImageModel:         strings.TrimSpace(r.GetString("llm.image.model")),
		ImageTimeoutRaw:    strings.TrimSpace(r.GetString("llm.image.request_timeout")),
		ImageOptions: llm.ImageProviderOptions{
			OpenAI:     loadAnyMapKeyFromReader(r, "llm.image.options.openai"),
			Gemini:     loadAnyMapKeyFromReader(r, "llm.image.options.gemini"),
			Cloudflare: loadAnyMapKeyFromReader(r, "llm.image.options.cloudflare"),
		},
		BedrockAWSKey:          firstNonEmpty(r.GetString("llm.bedrock.aws_key"), r.GetString("llm.aws.key")),
		BedrockAWSSecret:       firstNonEmpty(r.GetString("llm.bedrock.aws_secret"), r.GetString("llm.aws.secret")),
		BedrockAWSSessionToken: strings.TrimSpace(r.GetString("llm.bedrock.aws_session_token")),
		BedrockAWSProfile:      strings.TrimSpace(r.GetString("llm.bedrock.aws_profile")),
		BedrockAWSRegion:       firstNonEmpty(r.GetString("llm.bedrock.region"), r.GetString("llm.aws.region")),
		BedrockModelARN:        firstNonEmpty(r.GetString("llm.bedrock.model_arn"), r.GetString("llm.aws.bedrock_model_arn")),
		CloudflareAccountID: firstNonEmpty(
			r.GetString("llm.cloudflare.account_id"),
		),
		CloudflareAPIToken: firstNonEmpty(
			r.GetString("llm.cloudflare.api_token"),
		),
	}
}

func RuntimeValuesWithClientConfig(values RuntimeValues, cfg llmconfig.ClientConfig) RuntimeValues {
	out := values
	out.InferenceProvider = InferInferenceProvider(cfg.Provider, cfg.Endpoint)
	out.Provider = strings.TrimSpace(cfg.Provider)
	out.Endpoint = strings.TrimSpace(cfg.Endpoint)
	out.APIKey = strings.TrimSpace(cfg.APIKey)
	out.Model = strings.TrimSpace(cfg.Model)
	if cfg.ContextWindowTokens > 0 {
		out.ContextWindowRaw = strconv.FormatInt(cfg.ContextWindowTokens, 10)
	}
	out.Headers = cloneStringMap(cfg.Headers)
	if cfg.RequestTimeout > 0 {
		out.RequestTimeoutRaw = cfg.RequestTimeout.String()
	}
	return out
}

func ImageClientFromValues(values RuntimeValues) (llm.ImageClient, error) {
	meta := imageClientMetadataFromValues(values)
	apiKey := firstNonEmpty(values.ImageAPIKey, APIKeyForProviderWithValues(meta.Provider, values))
	requestTimeout, err := requestTimeoutFromValue(values.ImageTimeoutRaw, "llm.image.request_timeout")
	if err != nil {
		return nil, err
	}
	pricing, _, err := LoadPricingCatalog(values)
	if err != nil {
		return nil, err
	}
	c, err := uniaiProvider.New(uniaiProvider.Config{
		Provider:            meta.Provider,
		Endpoint:            strings.TrimSpace(meta.Endpoint),
		APIKey:              strings.TrimSpace(apiKey),
		Model:               strings.TrimSpace(meta.Model),
		Pricing:             pricing,
		RequestTimeout:      requestTimeout,
		CloudflareAccountID: firstNonEmpty(values.CloudflareAccountID),
		CloudflareAPIToken:  firstNonEmpty(values.CloudflareAPIToken, apiKey),
		CloudflareAPIBase:   strings.TrimSpace(meta.Endpoint),
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func ImageClientFromValuesWithStats(values RuntimeValues, logger *slog.Logger) (llm.ImageClient, error) {
	client, err := ImageClientFromValues(values)
	if err != nil {
		return nil, err
	}
	meta := imageClientMetadataFromValues(values)
	return llmstats.WrapRuntimeImageClient(client, meta.Provider, meta.Endpoint, meta.Model, logger), nil
}

func imageClientMetadataFromValues(values RuntimeValues) imageClientMetadata {
	if strings.TrimSpace(values.ImageProvider) == "" {
		if resolved, err := ResolveRuntimeValuesInferenceProvider(values); err == nil {
			values = resolved
		}
	}
	sourceProvider := strings.ToLower(firstNonEmpty(values.ImageProvider, values.Provider))
	provider := normalizeImageProviderForUniai(sourceProvider)
	return imageClientMetadata{
		Provider: provider,
		Endpoint: imageEndpointForValues(sourceProvider, provider, values),
		Model:    strings.TrimSpace(firstNonEmpty(values.ImageModel, values.Model)),
	}
}

func normalizeImageProviderForUniai(provider string) string {
	provider = normalizeProvider(provider)
	switch provider {
	case "openai_codex", "openai_custom", "openai_resp":
		return "openai"
	default:
		return provider
	}
}

func imageEndpointForValues(sourceProvider string, imageProvider string, values RuntimeValues) string {
	if endpoint := strings.TrimSpace(values.ImageEndpoint); endpoint != "" {
		return endpoint
	}
	if normalizeProvider(values.Provider) == "openai_codex" || normalizeProvider(sourceProvider) == "openai_codex" {
		return ""
	}
	if normalizeImageProviderForUniai(values.Provider) != imageProvider {
		return ""
	}
	return EndpointForProviderWithValues(imageProvider, values)
}

func RuntimeValuesFromViper() RuntimeValues {
	return RuntimeValuesFromReader(viper.GetViper())
}

func ModelFromViper() string {
	values := RuntimeValuesFromViper()
	if resolved, err := ResolveRuntimeValuesInferenceProvider(values); err == nil {
		values = resolved
	}
	return ModelForProviderWithValues(values.Provider, values)
}

func EndpointForProviderWithValues(provider string, values RuntimeValues) string {
	provider = normalizeProvider(provider)
	switch provider {
	case "openai_codex":
		return codexauth.DefaultAPIBase
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
	if normalizeInferenceProvider(values.InferenceProvider) == InferenceProviderMisterMorphPro {
		if apiKey, ok, err := proaccount.ReadSubscriptionAPIKey(values.FileStateDir); err == nil && ok {
			return apiKey
		}
		return ""
	}
	switch provider {
	case "openai_codex":
		return ""
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
	case "openai_codex":
		return firstNonEmpty(
			values.Model,
			codexauth.DefaultModel,
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
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	var temperature *float64
	if provider == "openai_codex" {
		if strings.TrimSpace(values.TemperatureRaw) != "" {
			slog.Warn("llm_temperature_ignored", "provider", provider, "field", "llm.temperature")
		}
	} else {
		temperature, err = optionalFloat64FromValue(values.TemperatureRaw, "llm.temperature")
		if err != nil {
			return nil, err
		}
	}
	reasoningEffort, err := reasoningEffortFromValue(values.ReasoningEffortRaw)
	if err != nil {
		return nil, err
	}
	var reasoningBudget *int
	if provider == "openai_codex" {
		if strings.TrimSpace(values.ReasoningBudgetRaw) != "" {
			slog.Warn("llm_reasoning_budget_ignored", "provider", provider, "field", "llm.reasoning_budget_tokens")
		}
	} else {
		reasoningBudget, err = optionalIntFromValue(values.ReasoningBudgetRaw, "llm.reasoning_budget_tokens")
		if err != nil {
			return nil, err
		}
	}
	pricing, _, err := LoadPricingCatalog(values)
	if err != nil {
		return nil, err
	}
	if provider == "openai_resp" && reasoningBudget != nil {
		slog.Warn("llm_reasoning_budget_ignored", "provider", provider, "field", "llm.reasoning_budget_tokens")
	}
	uniaiProviderName := uniaiChatProviderName(provider)
	switch provider {
	case "openai_codex":
		return codexProvider.New(codexProvider.Config{
			Endpoint:           strings.TrimSpace(cfg.Endpoint),
			Model:              strings.TrimSpace(cfg.Model),
			Headers:            cloneStringMap(cfg.Headers),
			Pricing:            pricing,
			RequestTimeout:     cfg.RequestTimeout,
			ToolsEmulationMode: toolsEmulationMode,
			ReasoningEffort:    reasoningEffort,
			StateDir:           strings.TrimSpace(values.FileStateDir),
		}), nil
	case "openai", "openai_resp", "openai_custom", "deepseek", "xai", "sakana", "gemini", "azure", "anthropic", "bedrock", "susanoo", "cloudflare":
		c, err := uniaiProvider.New(uniaiProvider.Config{
			Provider:           uniaiProviderName,
			InferenceProvider:  strings.TrimSpace(values.InferenceProvider),
			Endpoint:           strings.TrimSpace(cfg.Endpoint),
			APIKey:             strings.TrimSpace(cfg.APIKey),
			Model:              strings.TrimSpace(cfg.Model),
			Headers:            cloneStringMap(cfg.Headers),
			Pricing:            pricing,
			RequestTimeout:     cfg.RequestTimeout,
			CacheTTL:           strings.TrimSpace(values.CacheTTL),
			CacheKeyPrefix:     strings.TrimSpace(values.CacheKeyPrefix),
			ToolsEmulationMode: toolsEmulationMode,
			Temperature:        temperature,
			ReasoningEffort:    reasoningEffort,
			ReasoningBudget:    reasoningBudget,
			AzureAPIKey:        strings.TrimSpace(cfg.APIKey),
			AzureEndpoint:      strings.TrimSpace(cfg.Endpoint),
			AzureDeployment:    strings.TrimSpace(cfg.Model),
			AwsKey:             firstNonEmpty(values.BedrockAWSKey),
			AwsSecret:          firstNonEmpty(values.BedrockAWSSecret),
			AwsSessionToken:    firstNonEmpty(values.BedrockAWSSessionToken),
			AwsProfile:         firstNonEmpty(values.BedrockAWSProfile),
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
		if err != nil {
			return nil, err
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

func uniaiChatProviderName(provider string) string {
	provider = normalizeProvider(provider)
	switch provider {
	case "openai_custom":
		return "openai"
	case "sakana":
		return "openai_resp"
	default:
		return provider
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

func optionalNonNegativeInt64FromValue(raw, path string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", path, raw)
	}
	if v < 0 {
		return 0, fmt.Errorf("invalid %s %q (expected >= 0)", path, raw)
	}
	return v, nil
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

func loadAnyMapKeyFromReader(r ConfigReader, key string) map[string]any {
	var raw map[string]any
	if err := unmarshalKey(r, key, &raw); err == nil {
		return cloneAnyMap(raw)
	}
	if getter, ok := any(r).(interface {
		GetStringMap(string) map[string]any
	}); ok {
		return cloneAnyMap(getter.GetStringMap(key))
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

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		out[k] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
