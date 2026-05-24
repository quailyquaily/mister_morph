package agentsettings

import (
	"fmt"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

type LLMConfigFieldsPayload struct {
	InferenceProvider   string `json:"inference_provider"`
	Provider            string `json:"provider"`
	Endpoint            string `json:"endpoint"`
	Model               string `json:"model"`
	ContextWindowTokens string `json:"context_window_tokens"`
	APIKey              string `json:"api_key"`
	BedrockAWSKey       string `json:"bedrock_aws_key"`
	BedrockAWSSecret    string `json:"bedrock_aws_secret"`
	BedrockRegion       string `json:"bedrock_region"`
	BedrockModelARN     string `json:"bedrock_model_arn"`
	CloudflareAPIToken  string `json:"cloudflare_api_token"`
	CloudflareAccountID string `json:"cloudflare_account_id"`
	ReasoningEffort     string `json:"reasoning_effort"`
	ToolsEmulationMode  string `json:"tools_emulation_mode"`
}

type LLMProfileSettingsPayload struct {
	Name string `json:"name"`
	LLMConfigFieldsPayload
}

type LLMSettingsPayload struct {
	LLMConfigFieldsPayload
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
			InferenceProvider:   strings.TrimSpace(displayValues.InferenceProvider),
			Provider:            provider,
			Endpoint:            llmutil.EndpointForProviderWithValues(provider, displayValues),
			Model:               llmutil.ModelForProviderWithValues(provider, displayValues),
			ContextWindowTokens: strings.TrimSpace(displayValues.ContextWindowRaw),
			APIKey:              ResolvedAgentSettingsAPIKey(provider, strings.TrimSpace(displayValues.APIKey)),
			BedrockAWSKey:       strings.TrimSpace(displayValues.BedrockAWSKey),
			BedrockAWSSecret:    strings.TrimSpace(displayValues.BedrockAWSSecret),
			BedrockRegion:       strings.TrimSpace(displayValues.BedrockAWSRegion),
			BedrockModelARN:     strings.TrimSpace(displayValues.BedrockModelARN),
			CloudflareAPIToken:  ResolvedCloudflareToken(provider, strings.TrimSpace(displayValues.APIKey), strings.TrimSpace(displayValues.CloudflareAPIToken)),
			CloudflareAccountID: ResolvedCloudflareAccountID(provider, strings.TrimSpace(displayValues.CloudflareAccountID)),
			ReasoningEffort:     strings.TrimSpace(displayValues.ReasoningEffortRaw),
			ToolsEmulationMode:  strings.TrimSpace(displayValues.ToolsEmulationMode),
		},
		Profiles:         ProfileSettingsPayloadsFromMap(displayValues.Profiles, provider),
		FallbackProfiles: NormalizeNamedProfileSequence(displayValues.Routes.MainLoop.FallbackProfiles),
	}
	payload.LLMConfigFieldsPayload = SanitizeProviderSpecificLLMFields(payload.LLMConfigFieldsPayload, provider)
	return payload
}

func ProfileSettingsPayloadsFromMap(
	profiles map[string]llmutil.ProfileConfig,
	defaultProvider string,
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
		out = append(out, ProfileSettingsPayloadFromConfig(name, profiles[name], defaultProvider))
	}
	return out
}

func ProfileSettingsPayloadFromConfig(
	name string,
	cfg llmutil.ProfileConfig,
	defaultProvider string,
) LLMProfileSettingsPayload {
	effectiveProvider := FirstNonEmpty(strings.TrimSpace(cfg.Provider), defaultProvider)
	displayValues := llmutil.RuntimeValues{
		InferenceProvider: strings.TrimSpace(cfg.InferenceProvider),
		Provider:          effectiveProvider,
		Endpoint:          strings.TrimSpace(cfg.Endpoint),
	}
	if resolved, err := llmutil.ResolveRuntimeValuesInferenceProvider(displayValues); err == nil {
		effectiveProvider = FirstNonEmpty(strings.TrimSpace(resolved.Provider), effectiveProvider)
	}
	payload := LLMProfileSettingsPayload{
		Name: strings.TrimSpace(name),
		LLMConfigFieldsPayload: LLMConfigFieldsPayload{
			InferenceProvider:   strings.TrimSpace(cfg.InferenceProvider),
			Provider:            strings.TrimSpace(cfg.Provider),
			Endpoint:            strings.TrimSpace(cfg.Endpoint),
			Model:               strings.TrimSpace(cfg.Model),
			ContextWindowTokens: strings.TrimSpace(cfg.ContextWindowRaw),
			APIKey:              ResolvedAgentSettingsAPIKey(effectiveProvider, strings.TrimSpace(cfg.APIKey)),
			BedrockAWSKey:       strings.TrimSpace(cfg.Bedrock.AWSKey),
			BedrockAWSSecret:    strings.TrimSpace(cfg.Bedrock.AWSSecret),
			BedrockRegion:       strings.TrimSpace(cfg.Bedrock.Region),
			BedrockModelARN:     strings.TrimSpace(cfg.Bedrock.ModelARN),
			CloudflareAPIToken:  ResolvedCloudflareToken(effectiveProvider, strings.TrimSpace(cfg.APIKey), strings.TrimSpace(cfg.Cloudflare.APIToken)),
			CloudflareAccountID: ResolvedCloudflareAccountID(effectiveProvider, strings.TrimSpace(cfg.Cloudflare.AccountID)),
			ReasoningEffort:     strings.TrimSpace(cfg.ReasoningEffortRaw),
			ToolsEmulationMode:  strings.TrimSpace(cfg.ToolsEmulationMode),
		},
	}
	payload.LLMConfigFieldsPayload = SanitizeProviderSpecificLLMFields(payload.LLMConfigFieldsPayload, effectiveProvider)
	return payload
}

func SanitizeProviderSpecificLLMFields(
	fields LLMConfigFieldsPayload,
	effectiveProvider string,
) LLMConfigFieldsPayload {
	if strings.EqualFold(strings.TrimSpace(fields.InferenceProvider), llmutil.InferenceProviderMisterMorphPro) {
		fields.Provider = ""
		fields.Endpoint = ""
		fields.APIKey = ""
		fields.BedrockAWSKey = ""
		fields.BedrockAWSSecret = ""
		fields.BedrockRegion = ""
		fields.BedrockModelARN = ""
		fields.CloudflareAPIToken = ""
		fields.CloudflareAccountID = ""
		return fields
	}
	switch strings.ToLower(strings.TrimSpace(effectiveProvider)) {
	case "cloudflare":
		fields.APIKey = ""
		fields.BedrockAWSKey = ""
		fields.BedrockAWSSecret = ""
		fields.BedrockRegion = ""
		fields.BedrockModelARN = ""
	case "bedrock":
		fields.APIKey = ""
		fields.CloudflareAPIToken = ""
		fields.CloudflareAccountID = ""
	case "openai_codex":
		fields.Endpoint = ""
		fields.APIKey = ""
		fields.BedrockAWSKey = ""
		fields.BedrockAWSSecret = ""
		fields.BedrockRegion = ""
		fields.BedrockModelARN = ""
		fields.CloudflareAPIToken = ""
		fields.CloudflareAccountID = ""
	default:
		fields.BedrockAWSKey = ""
		fields.BedrockAWSSecret = ""
		fields.BedrockRegion = ""
		fields.BedrockModelARN = ""
		fields.CloudflareAPIToken = ""
		fields.CloudflareAccountID = ""
	}
	return fields
}

func ResolvedAgentSettingsAPIKey(provider, apiKey string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "openai_codex") {
		return ""
	}
	return strings.TrimSpace(apiKey)
}

func ResolvedCloudflareToken(provider, apiKey, apiToken string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "cloudflare") {
		return FirstNonEmpty(apiToken, apiKey)
	}
	if strings.EqualFold(strings.TrimSpace(provider), "openai_codex") {
		return ""
	}
	return strings.TrimSpace(apiToken)
}

func ResolvedCloudflareAccountID(provider, accountID string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "openai_codex") {
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

func ApplyLLMSettingsNonEmptyUpdate(
	current LLMSettingsPayload,
	incoming LLMSettingsPayload,
	includeProfiles bool,
) LLMSettingsPayload {
	merged := current
	if value := strings.TrimSpace(incoming.InferenceProvider); value != "" {
		merged.InferenceProvider = value
	} else if strings.TrimSpace(incoming.Provider) != "" || strings.TrimSpace(incoming.Endpoint) != "" {
		merged.InferenceProvider = ""
	}
	if value := strings.TrimSpace(incoming.Provider); value != "" {
		merged.Provider = value
	}
	if value := strings.TrimSpace(incoming.Endpoint); value != "" {
		merged.Endpoint = value
	}
	if value := strings.TrimSpace(incoming.Model); value != "" {
		merged.Model = value
	}
	if value := strings.TrimSpace(incoming.ContextWindowTokens); value != "" {
		merged.ContextWindowTokens = value
	}
	if value := strings.TrimSpace(incoming.APIKey); value != "" {
		merged.APIKey = value
	}
	if value := strings.TrimSpace(incoming.BedrockAWSKey); value != "" {
		merged.BedrockAWSKey = value
	}
	if value := strings.TrimSpace(incoming.BedrockAWSSecret); value != "" {
		merged.BedrockAWSSecret = value
	}
	if value := strings.TrimSpace(incoming.BedrockRegion); value != "" {
		merged.BedrockRegion = value
	}
	if value := strings.TrimSpace(incoming.BedrockModelARN); value != "" {
		merged.BedrockModelARN = value
	}
	if value := strings.TrimSpace(incoming.CloudflareAPIToken); value != "" {
		merged.CloudflareAPIToken = value
	}
	if value := strings.TrimSpace(incoming.CloudflareAccountID); value != "" {
		merged.CloudflareAccountID = value
	}
	if value := strings.TrimSpace(incoming.ReasoningEffort); value != "" {
		merged.ReasoningEffort = value
	}
	if value := strings.TrimSpace(incoming.ToolsEmulationMode); value != "" {
		merged.ToolsEmulationMode = value
	}
	if includeProfiles && len(incoming.Profiles) > 0 {
		merged.Profiles = append([]LLMProfileSettingsPayload(nil), incoming.Profiles...)
	}
	if includeProfiles && len(incoming.FallbackProfiles) > 0 {
		merged.FallbackProfiles = NormalizeNamedProfileSequence(incoming.FallbackProfiles)
	}
	merged.LLMConfigFieldsPayload = ResolveInferenceProviderSettingsFields(merged.LLMConfigFieldsPayload)
	merged.LLMConfigFieldsPayload = SanitizeProviderSpecificLLMFields(merged.LLMConfigFieldsPayload, merged.Provider)
	return merged
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

func NormalizeAgentSettingsProviderForOverride(provider string) string {
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
	inferenceProvider := strings.TrimSpace(req.InferenceProvider)
	if inferenceProvider == "" && !requestSetsRoute {
		inferenceProvider = current.InferenceProvider
	}
	values := llmutil.RuntimeValues{
		InferenceProvider: inferenceProvider,
		Provider:          FirstNonEmpty(strings.TrimSpace(req.Provider), current.Provider),
		Endpoint:          FirstNonEmpty(strings.TrimSpace(req.Endpoint), current.Endpoint),
		APIKey:            FirstNonEmpty(strings.TrimSpace(req.APIKey), current.APIKey),
		FileStateDir:      strings.TrimSpace(req.FileStateDir),
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
