package agentsettings

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
)

type EnvManagedField struct {
	Source   string `json:"source,omitempty"`
	EnvName  string `json:"env_name"`
	Value    string `json:"value,omitempty"`
	RawValue string `json:"raw_value,omitempty"`
}

type EnvManagedPayload struct {
	LLM         map[string]EnvManagedField            `json:"llm,omitempty"`
	LLMProfiles map[string]map[string]EnvManagedField `json:"llm_profiles,omitempty"`
}

// EffectiveRuntimeValues decodes one explicit config snapshot and then applies
// the process environment fields that are documented as runtime overrides.
// It never consults Viper's package-global instance.
func EffectiveRuntimeValues(reader llmutil.ConfigReader) (llmutil.RuntimeValues, error) {
	if reader == nil {
		return llmutil.RuntimeValues{}, fmt.Errorf("config reader is nil")
	}
	values, err := llmutil.RuntimeValuesFromReader(reader)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	applyRuntimeEnvironment(&values)
	return values, nil
}

func applyRuntimeEnvironment(values *llmutil.RuntimeValues) {
	if values == nil {
		return
	}
	overrides := []struct {
		name  string
		apply func(string)
	}{
		{"MISTER_MORPH_LLM_INFERENCE_PROVIDER", func(value string) { values.InferenceProvider = value }},
		{"MISTER_MORPH_LLM_PROVIDER", func(value string) { values.Provider = value }},
		{"MISTER_MORPH_LLM_ENDPOINT", func(value string) { values.Endpoint = value }},
		{"MISTER_MORPH_LLM_API_KEY", func(value string) { values.APIKey = value }},
		{"MISTER_MORPH_LLM_MODEL", func(value string) { values.Model = value }},
		{"MISTER_MORPH_LLM_CONTEXT_WINDOW_TOKENS", func(value string) { values.ContextWindowRaw = value }},
		{"MISTER_MORPH_LLM_AZURE_DEPLOYMENT", func(value string) { values.AzureDeployment = value }},
		{"MISTER_MORPH_LLM_REASONING_EFFORT", func(value string) { values.ReasoningEffortRaw = value }},
		{"MISTER_MORPH_LLM_TOOLS_EMULATION_MODE", func(value string) { values.ToolsEmulationMode = value }},
		{"MISTER_MORPH_LLM_BEDROCK_AWS_KEY", func(value string) { values.BedrockAWSKey = value }},
		{"MISTER_MORPH_LLM_BEDROCK_AWS_SECRET", func(value string) { values.BedrockAWSSecret = value }},
		{"MISTER_MORPH_LLM_BEDROCK_REGION", func(value string) { values.BedrockAWSRegion = value }},
		{"MISTER_MORPH_LLM_BEDROCK_MODEL_ARN", func(value string) { values.BedrockModelARN = value }},
		{"MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID", func(value string) { values.CloudflareAccountID = value }},
		{"MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN", func(value string) { values.CloudflareAPIToken = value }},
	}
	for _, override := range overrides {
		if value, ok := os.LookupEnv(override.name); ok {
			override.apply(strings.TrimSpace(value))
		}
	}
}

// ResolveConnectionTestValues combines the submitted settings with immutable
// runtime-only fields from reader and resolves secret references through the
// caller's explicit source.
func ResolveConnectionTestValues(
	reader llmutil.ConfigReader,
	snapshot LLMSettingsPayload,
	targetProfile string,
	source secref.Source,
) (llmutil.RuntimeValues, error) {
	base, err := EffectiveRuntimeValues(reader)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	values, err := runtimeValuesFromSettings(base, snapshot.LLMConfigFieldsPayload, source, false)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	targetProfile = strings.TrimSpace(targetProfile)
	if targetProfile == "" || strings.EqualFold(targetProfile, llmutil.RouteProfileDefault) {
		return values, nil
	}
	profile, ok := findProfile(snapshot.Profiles, targetProfile)
	if !ok {
		return llmutil.RuntimeValues{}, fmt.Errorf("missing profile %q", targetProfile)
	}
	profileValues, err := runtimeValuesFromSettings(base, profile.LLMConfigFieldsPayload, source, true)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	values.Profiles = map[string]llmutil.ProfileConfig{
		targetProfile: profileConfigFromValues(profileValues),
	}
	values.Routes.MainLoop = llmutil.RoutePolicyConfig{Profile: targetProfile}
	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return llmutil.RuntimeValues{}, err
	}
	return route.Values, nil
}

func runtimeValuesFromSettings(base llmutil.RuntimeValues, fields LLMConfigFieldsPayload, source secref.Source, profile bool) (llmutil.RuntimeValues, error) {
	resolve := func(value string) (string, error) {
		value = strings.TrimSpace(value)
		if value == "" {
			return "", nil
		}
		resolved, err := secref.ResolveString(context.Background(), value, source, secref.Options{EnvMissing: secref.EnvMissingError})
		if err != nil {
			if missingErr, ok := err.(secref.MissingEnvError); ok {
				return "", fmt.Errorf("missing env %q", strings.Join(missingErr.Names, ", "))
			}
			return "", err
		}
		return strings.TrimSpace(resolved.Value), nil
	}
	resolved := make([]string, 14)
	raw := []string{
		fields.InferenceProvider, fields.Provider, fields.Endpoint, fields.APIKey,
		fields.Model, fields.ContextWindowTokens, fields.CloudflareAPIToken,
		fields.CloudflareAccountID, fields.BedrockAWSKey, fields.BedrockAWSSecret,
		fields.BedrockRegion, fields.BedrockModelARN, fields.ReasoningEffort,
		fields.ToolsEmulationMode,
	}
	for i := range raw {
		value, err := resolve(raw[i])
		if err != nil {
			return llmutil.RuntimeValues{}, err
		}
		resolved[i] = value
	}
	provider := NormalizeAgentSettingsProvider(resolved[1])
	if profile {
		provider = NormalizeAgentSettingsProviderForOverride(resolved[1])
	}
	base.InferenceProvider = resolved[0]
	base.Provider = provider
	base.Endpoint = resolved[2]
	base.APIKey = resolved[3]
	base.Model = resolved[4]
	base.ContextWindowRaw = resolved[5]
	base.CloudflareAPIToken = resolved[6]
	base.CloudflareAccountID = resolved[7]
	base.BedrockAWSKey = resolved[8]
	base.BedrockAWSSecret = resolved[9]
	base.BedrockAWSRegion = resolved[10]
	base.BedrockModelARN = resolved[11]
	base.ReasoningEffortRaw = resolved[12]
	base.ToolsEmulationMode = resolved[13]
	return base, nil
}

func profileConfigFromValues(values llmutil.RuntimeValues) llmutil.ProfileConfig {
	return llmutil.ProfileConfig{
		InferenceProvider:  values.InferenceProvider,
		Provider:           values.Provider,
		Endpoint:           values.Endpoint,
		APIKey:             values.APIKey,
		Model:              values.Model,
		ContextWindowRaw:   values.ContextWindowRaw,
		ToolsEmulationMode: values.ToolsEmulationMode,
		ReasoningEffortRaw: values.ReasoningEffortRaw,
		Bedrock: struct {
			AWSKey          string `mapstructure:"aws_key" yaml:"aws_key"`
			AWSSecret       string `mapstructure:"aws_secret" yaml:"aws_secret"`
			AWSSessionToken string `mapstructure:"aws_session_token" yaml:"aws_session_token"`
			AWSProfile      string `mapstructure:"aws_profile" yaml:"aws_profile"`
			Region          string `mapstructure:"region" yaml:"region"`
			ModelARN        string `mapstructure:"model_arn" yaml:"model_arn"`
		}{
			AWSKey: values.BedrockAWSKey, AWSSecret: values.BedrockAWSSecret,
			Region: values.BedrockAWSRegion, ModelARN: values.BedrockModelARN,
		},
		Cloudflare: struct {
			AccountID string `mapstructure:"account_id" yaml:"account_id"`
			APIToken  string `mapstructure:"api_token" yaml:"api_token"`
		}{AccountID: values.CloudflareAccountID, APIToken: values.CloudflareAPIToken},
	}
}

func findProfile(profiles []LLMProfileSettingsPayload, target string) (LLMProfileSettingsPayload, bool) {
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Name) == target {
			return profile, true
		}
	}
	return LLMProfileSettingsPayload{}, false
}

func CurrentEnvManaged(provider string) EnvManagedPayload {
	return EnvManagedPayload{LLM: CurrentLLMEnvManagedFields(provider)}
}

func CurrentLLMEnvManagedFields(provider string) map[string]EnvManagedField {
	fields := map[string]EnvManagedField{}
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	usesLocalOAuth := normalizedProvider == "openai_codex" || normalizedProvider == "xai_oauth"
	add := func(key string, sensitive bool, names ...string) {
		if field, ok := ManagedEnvField(sensitive, names...); ok {
			fields[key] = field
		}
	}
	add("inference_provider", false, "MISTER_MORPH_LLM_INFERENCE_PROVIDER")
	add("provider", false, "MISTER_MORPH_LLM_PROVIDER")
	if !usesLocalOAuth {
		add("endpoint", false, "MISTER_MORPH_LLM_ENDPOINT")
	}
	if normalizedProvider == "azure" {
		add("model", false, "MISTER_MORPH_LLM_AZURE_DEPLOYMENT", "MISTER_MORPH_LLM_MODEL")
	} else {
		add("model", false, "MISTER_MORPH_LLM_MODEL")
	}
	add("context_window_tokens", false, "MISTER_MORPH_LLM_CONTEXT_WINDOW_TOKENS")
	switch normalizedProvider {
	case "cloudflare":
		add("cloudflare_api_token", true, "MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN", "MISTER_MORPH_LLM_API_KEY")
	case "bedrock":
	default:
		if !usesLocalOAuth {
			add("api_key", true, "MISTER_MORPH_LLM_API_KEY")
			add("cloudflare_api_token", true, "MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN")
		}
	}
	if !usesLocalOAuth {
		add("cloudflare_account_id", false, "MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID")
	}
	add("bedrock_aws_key", true, "MISTER_MORPH_LLM_BEDROCK_AWS_KEY")
	add("bedrock_aws_secret", true, "MISTER_MORPH_LLM_BEDROCK_AWS_SECRET")
	add("bedrock_region", false, "MISTER_MORPH_LLM_BEDROCK_REGION")
	add("bedrock_model_arn", false, "MISTER_MORPH_LLM_BEDROCK_MODEL_ARN")
	add("reasoning_effort", false, "MISTER_MORPH_LLM_REASONING_EFFORT")
	add("tools_emulation_mode", false, "MISTER_MORPH_LLM_TOOLS_EMULATION_MODE")
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func ManagedEnvField(sensitive bool, names ...string) (EnvManagedField, bool) {
	name, value, ok := FirstManagedEnv(names...)
	if !ok {
		return EnvManagedField{}, false
	}
	field := EnvManagedField{EnvName: name, RawValue: "${" + name + "}"}
	if !sensitive {
		field.Value = strings.TrimSpace(value)
	}
	return field, true
}

func FirstManagedEnv(names ...string) (string, string, bool) {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if value, ok := os.LookupEnv(name); ok {
			return name, value, true
		}
	}
	return "", "", false
}

func SanitizeManagedLLMFields(fields *LLMConfigFieldsPayload, envManaged map[string]EnvManagedField, effectiveProvider string) {
	if fields == nil {
		return
	}
	for key, target := range map[string]*string{
		"provider": &fields.Provider, "endpoint": &fields.Endpoint, "model": &fields.Model,
		"context_window_tokens": &fields.ContextWindowTokens,
		"cloudflare_account_id": &fields.CloudflareAccountID, "bedrock_region": &fields.BedrockRegion,
		"bedrock_model_arn": &fields.BedrockModelARN, "reasoning_effort": &fields.ReasoningEffort,
		"tools_emulation_mode": &fields.ToolsEmulationMode,
	} {
		if field, ok := envManaged[key]; ok && strings.TrimSpace(field.Value) != "" {
			*target = strings.TrimSpace(field.Value)
		}
	}
	RedactManagedLLMSecrets(fields, envManaged, effectiveProvider)
}

func RedactManagedLLMSecrets(fields *LLMConfigFieldsPayload, envManaged map[string]EnvManagedField, effectiveProvider string) {
	if fields == nil {
		return
	}
	if _, ok := envManaged["api_key"]; ok {
		fields.APIKey = ""
	}
	if _, ok := envManaged["bedrock_aws_key"]; ok {
		fields.BedrockAWSKey = ""
	}
	if _, ok := envManaged["bedrock_aws_secret"]; ok {
		fields.BedrockAWSSecret = ""
	}
	if _, ok := envManaged["cloudflare_api_token"]; ok {
		fields.CloudflareAPIToken = ""
		if strings.EqualFold(strings.TrimSpace(effectiveProvider), "cloudflare") {
			fields.APIKey = ""
		}
	}
}
