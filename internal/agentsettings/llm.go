package agentsettings

import (
	"fmt"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

type LLMConfigFieldsPayload struct {
	InferenceProvider      string            `json:"inference_provider"`
	Provider               string            `json:"provider"`
	Endpoint               string            `json:"endpoint"`
	Model                  string            `json:"model"`
	ContextWindowTokens    string            `json:"context_window_tokens"`
	SupportsImageParts     string            `json:"supports_image_parts"`
	Headers                map[string]string `json:"headers"`
	CacheTTL               string            `json:"cache_ttl"`
	CacheKeyPrefix         string            `json:"cache_key_prefix"`
	RequestTimeout         string            `json:"request_timeout"`
	Temperature            string            `json:"temperature"`
	ReasoningBudgetTokens  string            `json:"reasoning_budget_tokens"`
	APIKey                 string            `json:"api_key"`
	AzureDeployment        string            `json:"azure_deployment"`
	BedrockAWSKey          string            `json:"bedrock_aws_key"`
	BedrockAWSSecret       string            `json:"bedrock_aws_secret"`
	BedrockAWSSessionToken string            `json:"bedrock_aws_session_token"`
	BedrockAWSProfile      string            `json:"bedrock_aws_profile"`
	BedrockRegion          string            `json:"bedrock_region"`
	BedrockModelARN        string            `json:"bedrock_model_arn"`
	CloudflareAPIToken     string            `json:"cloudflare_api_token"`
	CloudflareAccountID    string            `json:"cloudflare_account_id"`
	ReasoningEffort        string            `json:"reasoning_effort"`
	ToolsEmulationMode     string            `json:"tools_emulation_mode"`
}

type LLMProfileSettingsPayload struct {
	Name string `json:"name"`
	LLMConfigFieldsPayload
}

type LLMSettingsPayload struct {
	LLMConfigFieldsPayload
	CurrentProfile   string                      `json:"current_profile,omitempty"`
	Profiles         []LLMProfileSettingsPayload `json:"profiles,omitempty"`
	FallbackProfiles []string                    `json:"fallback_profiles,omitempty"`
}

type ModelLookupRequest struct {
	InferenceProvider string
	Provider          string
	Endpoint          string
	APIKey            string
	FileStateDir      string
}

type ModelLookupConfig struct {
	Endpoint string
	APIKey   string
}

func SettingsPayloadFromRuntimeValues(values llmutil.RuntimeValues) LLMSettingsPayload {
	displayValues := values
	if resolved, err := llmutil.ResolveRuntimeValuesInferenceProvider(displayValues); err == nil {
		displayValues = resolved
	}
	provider := strings.TrimSpace(displayValues.Provider)
	payload := LLMSettingsPayload{
		LLMConfigFieldsPayload: LLMConfigFieldsPayload{
			InferenceProvider:      strings.TrimSpace(displayValues.InferenceProvider),
			Provider:               provider,
			Endpoint:               llmutil.EndpointForProviderWithValues(provider, displayValues),
			Model:                  llmutil.ModelForProviderWithValues(provider, displayValues),
			ContextWindowTokens:    strings.TrimSpace(displayValues.ContextWindowRaw),
			SupportsImageParts:     optionalBoolString(displayValues.SupportsImageParts),
			Headers:                displayValues.Headers,
			CacheTTL:               strings.TrimSpace(displayValues.CacheTTL),
			CacheKeyPrefix:         strings.TrimSpace(displayValues.CacheKeyPrefix),
			RequestTimeout:         strings.TrimSpace(displayValues.RequestTimeoutRaw),
			Temperature:            strings.TrimSpace(displayValues.TemperatureRaw),
			ReasoningBudgetTokens:  strings.TrimSpace(displayValues.ReasoningBudgetRaw),
			APIKey:                 ResolvedAgentSettingsAPIKey(provider, strings.TrimSpace(displayValues.APIKey)),
			AzureDeployment:        strings.TrimSpace(displayValues.AzureDeployment),
			BedrockAWSKey:          strings.TrimSpace(displayValues.BedrockAWSKey),
			BedrockAWSSecret:       strings.TrimSpace(displayValues.BedrockAWSSecret),
			BedrockAWSSessionToken: strings.TrimSpace(displayValues.BedrockAWSSessionToken),
			BedrockAWSProfile:      strings.TrimSpace(displayValues.BedrockAWSProfile),
			BedrockRegion:          strings.TrimSpace(displayValues.BedrockAWSRegion),
			BedrockModelARN:        strings.TrimSpace(displayValues.BedrockModelARN),
			CloudflareAPIToken:     ResolvedCloudflareToken(provider, strings.TrimSpace(displayValues.APIKey), strings.TrimSpace(displayValues.CloudflareAPIToken)),
			CloudflareAccountID:    ResolvedCloudflareAccountID(provider, strings.TrimSpace(displayValues.CloudflareAccountID)),
			ReasoningEffort:        strings.TrimSpace(displayValues.ReasoningEffortRaw),
			ToolsEmulationMode:     strings.TrimSpace(displayValues.ToolsEmulationMode),
		},
		CurrentProfile:   strings.TrimSpace(displayValues.Routes.MainLoop.Profile),
		Profiles:         ProfileSettingsPayloadsFromMap(displayValues.Profiles),
		FallbackProfiles: NormalizeNamedProfileSequence(displayValues.Routes.MainLoop.FallbackProfiles),
	}
	payload.LLMConfigFieldsPayload = SanitizeProviderSpecificLLMFields(payload.LLMConfigFieldsPayload, provider)
	return payload
}

func ProfileSettingsPayloadsFromMap(
	profiles map[string]llmutil.ProfileConfig,
) []LLMProfileSettingsPayload {
	if len(profiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	sort.Strings(names)
	out := make([]LLMProfileSettingsPayload, 0, len(names))
	for _, name := range names {
		out = append(out, ProfileSettingsPayloadFromConfig(name, profiles[name]))
	}
	return out
}

func ProfileSettingsPayloadFromConfig(
	name string,
	cfg llmutil.ProfileConfig,
) LLMProfileSettingsPayload {
	effectiveProvider := strings.TrimSpace(cfg.Provider)
	displayValues := llmutil.RuntimeValues{
		InferenceProvider: strings.TrimSpace(cfg.InferenceProvider),
		Provider:          effectiveProvider,
		Endpoint:          strings.TrimSpace(cfg.Endpoint),
	}
	if resolved, err := llmutil.ResolveRuntimeValuesInferenceProvider(displayValues); err == nil {
		effectiveProvider = strings.TrimSpace(resolved.Provider)
	}
	payload := LLMProfileSettingsPayload{
		Name: strings.TrimSpace(name),
		LLMConfigFieldsPayload: LLMConfigFieldsPayload{
			InferenceProvider:      strings.TrimSpace(cfg.InferenceProvider),
			Provider:               strings.TrimSpace(cfg.Provider),
			Endpoint:               strings.TrimSpace(cfg.Endpoint),
			Model:                  strings.TrimSpace(cfg.Model),
			ContextWindowTokens:    strings.TrimSpace(cfg.ContextWindowRaw),
			SupportsImageParts:     optionalBoolString(cfg.SupportsImageParts),
			Headers:                cfg.Headers,
			CacheTTL:               strings.TrimSpace(cfg.CacheTTL),
			CacheKeyPrefix:         strings.TrimSpace(cfg.CacheKeyPrefix),
			RequestTimeout:         strings.TrimSpace(cfg.RequestTimeoutRaw),
			Temperature:            strings.TrimSpace(cfg.TemperatureRaw),
			ReasoningBudgetTokens:  strings.TrimSpace(cfg.ReasoningBudgetRaw),
			APIKey:                 ResolvedAgentSettingsAPIKey(effectiveProvider, strings.TrimSpace(cfg.APIKey)),
			AzureDeployment:        strings.TrimSpace(cfg.Azure.Deployment),
			BedrockAWSKey:          strings.TrimSpace(cfg.Bedrock.AWSKey),
			BedrockAWSSecret:       strings.TrimSpace(cfg.Bedrock.AWSSecret),
			BedrockAWSSessionToken: strings.TrimSpace(cfg.Bedrock.AWSSessionToken),
			BedrockAWSProfile:      strings.TrimSpace(cfg.Bedrock.AWSProfile),
			BedrockRegion:          strings.TrimSpace(cfg.Bedrock.Region),
			BedrockModelARN:        strings.TrimSpace(cfg.Bedrock.ModelARN),
			CloudflareAPIToken:     ResolvedCloudflareToken(effectiveProvider, strings.TrimSpace(cfg.APIKey), strings.TrimSpace(cfg.Cloudflare.APIToken)),
			CloudflareAccountID:    ResolvedCloudflareAccountID(effectiveProvider, strings.TrimSpace(cfg.Cloudflare.AccountID)),
			ReasoningEffort:        strings.TrimSpace(cfg.ReasoningEffortRaw),
			ToolsEmulationMode:     strings.TrimSpace(cfg.ToolsEmulationMode),
		},
	}
	payload.LLMConfigFieldsPayload = SanitizeProviderSpecificLLMFields(payload.LLMConfigFieldsPayload, effectiveProvider)
	return payload
}

func optionalBoolString(value *bool) string {
	if value == nil {
		return ""
	}
	if *value {
		return "true"
	}
	return "false"
}

func SanitizeProviderSpecificLLMFields(
	fields LLMConfigFieldsPayload,
	effectiveProvider string,
) LLMConfigFieldsPayload {
	isMisterMorphPro := strings.EqualFold(strings.TrimSpace(fields.InferenceProvider), llmutil.InferenceProviderMisterMorphPro)
	if isMisterMorphPro {
		fields.Provider = ""
		fields.Endpoint = ""
		fields.APIKey = ""
	}
	provider := strings.ToLower(strings.TrimSpace(effectiveProvider))
	if isMisterMorphPro || provider != "bedrock" {
		fields.BedrockAWSKey = ""
		fields.BedrockAWSSecret = ""
		fields.BedrockAWSSessionToken = ""
		fields.BedrockAWSProfile = ""
		fields.BedrockRegion = ""
		fields.BedrockModelARN = ""
	}
	if isMisterMorphPro || provider != "cloudflare" {
		fields.CloudflareAPIToken = ""
		fields.CloudflareAccountID = ""
	}
	if isMisterMorphPro {
		return fields
	}
	switch provider {
	case "cloudflare", "bedrock":
		fields.APIKey = ""
	case "xai_oauth":
		fields.Endpoint = ""
		fields.APIKey = ""
	}
	return fields
}

func ResolvedAgentSettingsAPIKey(provider, apiKey string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "xai_oauth":
		return ""
	}
	return strings.TrimSpace(apiKey)
}

func ResolvedCloudflareToken(provider, apiKey, apiToken string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "cloudflare") {
		return FirstNonEmpty(apiToken, apiKey)
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai_codex", "xai_oauth":
		return ""
	}
	return strings.TrimSpace(apiToken)
}

func ResolvedCloudflareAccountID(provider, accountID string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai_codex", "xai_oauth":
		return ""
	}
	return strings.TrimSpace(accountID)
}

func ResolveInferenceProviderSettingsFields(fields LLMConfigFieldsPayload) LLMConfigFieldsPayload {
	if strings.TrimSpace(fields.InferenceProvider) == "" {
		return fields
	}
	values := llmutil.RuntimeValues{
		InferenceProvider: strings.TrimSpace(fields.InferenceProvider),
		Provider:          strings.TrimSpace(fields.Provider),
		Endpoint:          strings.TrimSpace(fields.Endpoint),
	}
	resolved, err := llmutil.ResolveRuntimeValuesInferenceProvider(values)
	if err != nil {
		return fields
	}
	fields.InferenceProvider = strings.TrimSpace(resolved.InferenceProvider)
	fields.Provider = strings.TrimSpace(resolved.Provider)
	fields.Endpoint = strings.TrimSpace(resolved.Endpoint)
	return fields
}

func NormalizeNamedProfileSequence(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func NormalizeAgentSettingsProvider(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	switch value {
	case "", "openai_compatible":
		return "openai"
	default:
		return value
	}
}

func NormalizeAgentSettingsProfileProvider(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	switch value {
	case "":
		return ""
	case "openai_compatible":
		return "openai"
	default:
		return value
	}
}

func ResolveOpenAICompatibleModelLookup(
	current LLMSettingsPayload,
	req ModelLookupRequest,
	resolveField func(string) (string, error),
) (ModelLookupConfig, error) {
	requestSetsRoute := strings.TrimSpace(req.InferenceProvider) != "" ||
		strings.TrimSpace(req.Provider) != "" ||
		strings.TrimSpace(req.Endpoint) != ""
	values := llmutil.RuntimeValues{
		FileStateDir: strings.TrimSpace(req.FileStateDir),
	}
	if requestSetsRoute {
		values.InferenceProvider = strings.TrimSpace(req.InferenceProvider)
		values.Provider = strings.TrimSpace(req.Provider)
		values.Endpoint = strings.TrimSpace(req.Endpoint)
		values.APIKey = strings.TrimSpace(req.APIKey)
	} else {
		values.InferenceProvider = strings.TrimSpace(current.InferenceProvider)
		values.Provider = strings.TrimSpace(current.Provider)
		values.Endpoint = strings.TrimSpace(current.Endpoint)
		values.APIKey = FirstNonEmpty(strings.TrimSpace(req.APIKey), current.APIKey)
	}
	if resolveField != nil {
		var err error
		values.InferenceProvider, err = resolveField(values.InferenceProvider)
		if err != nil {
			return ModelLookupConfig{}, err
		}
		values.Provider, err = resolveField(values.Provider)
		if err != nil {
			return ModelLookupConfig{}, err
		}
		values.Endpoint, err = resolveField(values.Endpoint)
		if err != nil {
			return ModelLookupConfig{}, err
		}
		values.APIKey, err = resolveField(values.APIKey)
		if err != nil {
			return ModelLookupConfig{}, err
		}
	}
	resolved, err := llmutil.ResolveRuntimeValuesInferenceProvider(values)
	if err != nil {
		return ModelLookupConfig{}, err
	}
	provider := strings.TrimSpace(resolved.Provider)
	endpoint := strings.TrimSpace(llmutil.EndpointForProviderWithValues(provider, resolved))
	if endpoint == "" {
		if info, ok := llmutil.InferenceProviderInfoByValue(resolved.InferenceProvider); ok && info.RequiresAPIBase {
			return ModelLookupConfig{}, fmt.Errorf("api base is required")
		}
	}
	apiKey := strings.TrimSpace(llmutil.APIKeyForProviderWithValues(provider, resolved))
	if apiKey == "" {
		return ModelLookupConfig{}, fmt.Errorf("api key is required")
	}
	return ModelLookupConfig{
		Endpoint: endpoint,
		APIKey:   apiKey,
	}, nil
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
