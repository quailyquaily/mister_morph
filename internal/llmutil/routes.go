package llmutil

import (
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/spf13/viper"
)

const (
	RoutePurposeMainLoop    = "main_loop"
	RoutePurposeAddressing  = "addressing"
	RoutePurposeHeartbeat   = "heartbeat"
	RoutePurposePlanCreate  = "plan_create"
	RoutePurposeMemoryDraft = "memory_draft"
	RouteProfileDefault     = "default"
)

type ProfileConfig struct {
	Provider           string `mapstructure:"provider"`
	Endpoint           string `mapstructure:"endpoint"`
	APIKey             string `mapstructure:"api_key"`
	Model              string `mapstructure:"model"`
	RequestTimeoutRaw  string `mapstructure:"request_timeout"`
	ToolsEmulationMode string `mapstructure:"tools_emulation_mode"`
	TemperatureRaw     string `mapstructure:"temperature"`
	ReasoningEffortRaw string `mapstructure:"reasoning_effort"`
	ReasoningBudgetRaw string `mapstructure:"reasoning_budget_tokens"`
	Azure              struct {
		Deployment string `mapstructure:"deployment"`
	} `mapstructure:"azure"`
	Bedrock struct {
		AWSKey    string `mapstructure:"aws_key"`
		AWSSecret string `mapstructure:"aws_secret"`
		Region    string `mapstructure:"region"`
		ModelARN  string `mapstructure:"model_arn"`
	} `mapstructure:"bedrock"`
	Cloudflare struct {
		AccountID string `mapstructure:"account_id"`
		APIToken  string `mapstructure:"api_token"`
	} `mapstructure:"cloudflare"`
}

type PurposeRoutes struct {
	MainLoop    string `mapstructure:"main_loop"`
	Addressing  string `mapstructure:"addressing"`
	Heartbeat   string `mapstructure:"heartbeat"`
	PlanCreate  string `mapstructure:"plan_create"`
	MemoryDraft string `mapstructure:"memory_draft"`
}

type RoutesConfig struct {
	PurposeRoutes `mapstructure:",squash"`
}

type ResolvedRoute struct {
	Purpose      string
	Profile      string
	Values       RuntimeValues
	ClientConfig llmconfig.ClientConfig
}

func (r ResolvedRoute) SameProfile(other ResolvedRoute) bool {
	return strings.TrimSpace(r.Profile) == strings.TrimSpace(other.Profile)
}

func ResolveRoute(values RuntimeValues, purpose string) (ResolvedRoute, error) {
	purpose = normalizeRoutePurpose(purpose)
	if !isSupportedRoutePurpose(purpose) {
		return ResolvedRoute{}, fmt.Errorf("unsupported llm route purpose %q", strings.TrimSpace(purpose))
	}
	profileName := resolveRouteProfile(values.Routes, purpose)
	if profileName == "" {
		profileName = RouteProfileDefault
	}

	resolvedValues := cloneRuntimeValuesForRoute(values)
	if profileName != RouteProfileDefault {
		override, ok := values.Profiles[profileName]
		if !ok {
			return ResolvedRoute{}, fmt.Errorf("llm route %s targets missing profile %q", purpose, profileName)
		}
		resolvedValues = applyProfileOverride(resolvedValues, override)
	}

	requestTimeout, err := requestTimeoutFromValue(resolvedValues.RequestTimeoutRaw, "llm.request_timeout")
	if err != nil {
		return ResolvedRoute{}, err
	}
	provider := normalizeProvider(resolvedValues.Provider)
	cfg := llmconfig.ClientConfig{
		Provider:       provider,
		Endpoint:       EndpointForProviderWithValues(provider, resolvedValues),
		APIKey:         APIKeyForProviderWithValues(provider, resolvedValues),
		Model:          ModelForProviderWithValues(provider, resolvedValues),
		RequestTimeout: requestTimeout,
	}
	return ResolvedRoute{
		Purpose:      purpose,
		Profile:      profileName,
		Values:       resolvedValues,
		ClientConfig: cfg,
	}, nil
}

func loadLLMProfilesFromReader(r ConfigReader) map[string]ProfileConfig {
	raw := map[string]ProfileConfig{}
	if err := unmarshalKey(r, "llm.profiles", &raw); err != nil || len(raw) == 0 {
		return nil
	}
	out := make(map[string]ProfileConfig, len(raw))
	for name, cfg := range raw {
		key := strings.TrimSpace(name)
		if key == "" {
			continue
		}
		out[key] = normalizeProfileConfig(cfg)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func loadLLMRoutesFromReader(r ConfigReader) RoutesConfig {
	var routes RoutesConfig
	_ = unmarshalKey(r, "llm.routes", &routes)
	return normalizeRoutesConfig(routes)
}

func unmarshalKey(r ConfigReader, key string, target any) error {
	if r == nil {
		return fmt.Errorf("config reader is nil")
	}
	switch u := any(r).(type) {
	case interface{ UnmarshalKey(string, any) error }:
		return u.UnmarshalKey(key, target)
	case interface {
		UnmarshalKey(string, any, ...viper.DecoderConfigOption) error
	}:
		return u.UnmarshalKey(key, target)
	default:
		return fmt.Errorf("config reader does not support UnmarshalKey")
	}
}

func normalizeProfileConfig(cfg ProfileConfig) ProfileConfig {
	cfg.Provider = strings.TrimSpace(cfg.Provider)
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.RequestTimeoutRaw = strings.TrimSpace(cfg.RequestTimeoutRaw)
	cfg.ToolsEmulationMode = strings.TrimSpace(cfg.ToolsEmulationMode)
	cfg.TemperatureRaw = strings.TrimSpace(cfg.TemperatureRaw)
	cfg.ReasoningEffortRaw = strings.TrimSpace(cfg.ReasoningEffortRaw)
	cfg.ReasoningBudgetRaw = strings.TrimSpace(cfg.ReasoningBudgetRaw)
	cfg.Azure.Deployment = strings.TrimSpace(cfg.Azure.Deployment)
	cfg.Bedrock.AWSKey = strings.TrimSpace(cfg.Bedrock.AWSKey)
	cfg.Bedrock.AWSSecret = strings.TrimSpace(cfg.Bedrock.AWSSecret)
	cfg.Bedrock.Region = strings.TrimSpace(cfg.Bedrock.Region)
	cfg.Bedrock.ModelARN = strings.TrimSpace(cfg.Bedrock.ModelARN)
	cfg.Cloudflare.AccountID = strings.TrimSpace(cfg.Cloudflare.AccountID)
	cfg.Cloudflare.APIToken = strings.TrimSpace(cfg.Cloudflare.APIToken)
	return cfg
}

func normalizeRoutesConfig(cfg RoutesConfig) RoutesConfig {
	cfg.PurposeRoutes = normalizePurposeRoutes(cfg.PurposeRoutes)
	return cfg
}

func normalizePurposeRoutes(cfg PurposeRoutes) PurposeRoutes {
	cfg.MainLoop = strings.TrimSpace(cfg.MainLoop)
	cfg.Addressing = strings.TrimSpace(cfg.Addressing)
	cfg.Heartbeat = strings.TrimSpace(cfg.Heartbeat)
	cfg.PlanCreate = strings.TrimSpace(cfg.PlanCreate)
	cfg.MemoryDraft = strings.TrimSpace(cfg.MemoryDraft)
	return cfg
}

func resolveRouteProfile(routes RoutesConfig, purpose string) string {
	return routeTargetForPurpose(routes.PurposeRoutes, purpose)
}

func routeTargetForPurpose(routes PurposeRoutes, purpose string) string {
	switch purpose {
	case RoutePurposeMainLoop:
		return strings.TrimSpace(routes.MainLoop)
	case RoutePurposeAddressing:
		return strings.TrimSpace(routes.Addressing)
	case RoutePurposeHeartbeat:
		return strings.TrimSpace(routes.Heartbeat)
	case RoutePurposePlanCreate:
		return strings.TrimSpace(routes.PlanCreate)
	case RoutePurposeMemoryDraft:
		return strings.TrimSpace(routes.MemoryDraft)
	default:
		return ""
	}
}

func normalizeRoutePurpose(purpose string) string {
	return strings.ToLower(strings.TrimSpace(purpose))
}

func isSupportedRoutePurpose(purpose string) bool {
	switch purpose {
	case RoutePurposeMainLoop, RoutePurposeAddressing, RoutePurposeHeartbeat, RoutePurposePlanCreate, RoutePurposeMemoryDraft:
		return true
	default:
		return false
	}
}

func cloneRuntimeValuesForRoute(values RuntimeValues) RuntimeValues {
	out := values
	out.Profiles = nil
	out.Routes = RoutesConfig{}
	return out
}

func applyProfileOverride(base RuntimeValues, override ProfileConfig) RuntimeValues {
	out := cloneRuntimeValuesForRoute(base)
	applyStringOverride(&out.Provider, override.Provider)
	applyStringOverride(&out.Endpoint, override.Endpoint)
	applyStringOverride(&out.APIKey, override.APIKey)
	applyStringOverride(&out.Model, override.Model)
	applyStringOverride(&out.RequestTimeoutRaw, override.RequestTimeoutRaw)
	applyStringOverride(&out.ToolsEmulationMode, override.ToolsEmulationMode)
	applyStringOverride(&out.TemperatureRaw, override.TemperatureRaw)
	applyStringOverride(&out.ReasoningEffortRaw, override.ReasoningEffortRaw)
	applyStringOverride(&out.ReasoningBudgetRaw, override.ReasoningBudgetRaw)
	applyStringOverride(&out.AzureDeployment, override.Azure.Deployment)
	applyStringOverride(&out.BedrockAWSKey, override.Bedrock.AWSKey)
	applyStringOverride(&out.BedrockAWSSecret, override.Bedrock.AWSSecret)
	applyStringOverride(&out.BedrockAWSRegion, override.Bedrock.Region)
	applyStringOverride(&out.BedrockModelARN, override.Bedrock.ModelARN)
	applyStringOverride(&out.CloudflareAccountID, override.Cloudflare.AccountID)
	applyStringOverride(&out.CloudflareAPIToken, override.Cloudflare.APIToken)
	return out
}

func applyStringOverride(dst *string, value string) {
	if dst == nil {
		return
	}
	if value = strings.TrimSpace(value); value != "" {
		*dst = value
	}
}

