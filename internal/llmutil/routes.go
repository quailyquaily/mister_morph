package llmutil

import (
	"fmt"
	"sort"
	"strconv"
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
	Provider           string            `mapstructure:"provider"`
	Endpoint           string            `mapstructure:"endpoint"`
	APIKey             string            `mapstructure:"api_key"`
	Model              string            `mapstructure:"model"`
	Headers            map[string]string `mapstructure:"headers"`
	CacheTTL           string            `mapstructure:"cache_ttl"`
	CacheKeyPrefix     string            `mapstructure:"cache_key_prefix"`
	RequestTimeoutRaw  string            `mapstructure:"request_timeout"`
	ToolsEmulationMode string            `mapstructure:"tools_emulation_mode"`
	TemperatureRaw     string            `mapstructure:"temperature"`
	ReasoningEffortRaw string            `mapstructure:"reasoning_effort"`
	ReasoningBudgetRaw string            `mapstructure:"reasoning_budget_tokens"`
	Azure              struct {
		Deployment string `mapstructure:"deployment"`
	} `mapstructure:"azure"`
	Bedrock struct {
		AWSKey          string `mapstructure:"aws_key"`
		AWSSecret       string `mapstructure:"aws_secret"`
		AWSSessionToken string `mapstructure:"aws_session_token"`
		AWSProfile      string `mapstructure:"aws_profile"`
		Region          string `mapstructure:"region"`
		ModelARN        string `mapstructure:"model_arn"`
	} `mapstructure:"bedrock"`
	Cloudflare struct {
		AccountID string `mapstructure:"account_id"`
		APIToken  string `mapstructure:"api_token"`
	} `mapstructure:"cloudflare"`
}

type RouteCandidateConfig struct {
	Profile string `mapstructure:"profile"`
	Weight  int    `mapstructure:"weight"`
}

type RoutePolicyConfig struct {
	Profile          string                 `mapstructure:"profile"`
	Candidates       []RouteCandidateConfig `mapstructure:"candidates"`
	FallbackProfiles []string               `mapstructure:"fallback_profiles"`
}

type PurposeRoutes struct {
	MainLoop    RoutePolicyConfig `mapstructure:"main_loop"`
	Addressing  RoutePolicyConfig `mapstructure:"addressing"`
	Heartbeat   RoutePolicyConfig `mapstructure:"heartbeat"`
	PlanCreate  RoutePolicyConfig `mapstructure:"plan_create"`
	MemoryDraft RoutePolicyConfig `mapstructure:"memory_draft"`
}

type RoutesConfig struct {
	PurposeRoutes `mapstructure:",squash"`
	ParseErr      error `mapstructure:"-"`
}

type ResolvedCandidate struct {
	Profile      string
	Values       RuntimeValues
	ClientConfig llmconfig.ClientConfig
	Weight       int
}

type ResolvedFallback struct {
	Profile      string
	Values       RuntimeValues
	ClientConfig llmconfig.ClientConfig
}

type ResolvedProfile struct {
	Name         string
	Values       RuntimeValues
	ClientConfig llmconfig.ClientConfig
}

type ResolvedRoute struct {
	Purpose      string
	Identity     string
	Profile      string
	Values       RuntimeValues
	ClientConfig llmconfig.ClientConfig
	Candidates   []ResolvedCandidate
	Fallbacks    []ResolvedFallback
}

func (r ResolvedRoute) SameProfile(other ResolvedRoute) bool {
	if strings.TrimSpace(r.Identity) != "" || strings.TrimSpace(other.Identity) != "" {
		return strings.TrimSpace(r.Identity) == strings.TrimSpace(other.Identity)
	}
	return strings.TrimSpace(r.Profile) == strings.TrimSpace(other.Profile)
}

func ResolveRoute(values RuntimeValues, purpose string) (ResolvedRoute, error) {
	purpose = normalizeRoutePurpose(purpose)
	if !isSupportedRoutePurpose(purpose) {
		return ResolvedRoute{}, fmt.Errorf("unsupported llm route purpose %q", strings.TrimSpace(purpose))
	}
	if values.Routes.ParseErr != nil {
		return ResolvedRoute{}, values.Routes.ParseErr
	}

	policy := resolveRoutePolicy(values.Routes, purpose)
	if err := validateRoutePolicy(policy, purpose); err != nil {
		return ResolvedRoute{}, err
	}

	if len(policy.Candidates) > 0 {
		candidates, err := resolveRouteCandidates(values, policy.Candidates, purpose)
		if err != nil {
			return ResolvedRoute{}, err
		}
		primary := displayCandidate(candidates)
		fallbacks, err := resolveFallbacks(values, policy.FallbackProfiles, candidateProfiles(candidates))
		if err != nil {
			return ResolvedRoute{}, err
		}
		return ResolvedRoute{
			Purpose:      purpose,
			Identity:     routePolicyIdentity(policy),
			Profile:      primary.Profile,
			Values:       primary.Values,
			ClientConfig: primary.ClientConfig,
			Candidates:   candidates,
			Fallbacks:    fallbacks,
		}, nil
	}

	profileName := strings.TrimSpace(policy.Profile)
	if profileName == "" {
		profileName = RouteProfileDefault
	}
	resolvedValues, err := resolveProfileValues(values, profileName)
	if err != nil {
		return ResolvedRoute{}, err
	}
	cfg, err := resolvedClientConfig(resolvedValues)
	if err != nil {
		return ResolvedRoute{}, err
	}
	fallbacks, err := resolveFallbacks(values, policy.FallbackProfiles, []string{profileName})
	if err != nil {
		return ResolvedRoute{}, err
	}
	return ResolvedRoute{
		Purpose:      purpose,
		Identity:     routePolicyIdentity(policy),
		Profile:      profileName,
		Values:       resolvedValues,
		ClientConfig: cfg,
		Fallbacks:    fallbacks,
	}, nil
}

func ResolveProfile(values RuntimeValues, profileName string) (ResolvedProfile, error) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		profileName = RouteProfileDefault
	}
	resolvedValues, err := resolveProfileValues(values, profileName)
	if err != nil {
		return ResolvedProfile{}, err
	}
	cfg, err := resolvedClientConfig(resolvedValues)
	if err != nil {
		return ResolvedProfile{}, err
	}
	return ResolvedProfile{
		Name:         profileName,
		Values:       resolvedValues,
		ClientConfig: cfg,
	}, nil
}

func ListProfiles(values RuntimeValues) ([]ResolvedProfile, error) {
	names := make([]string, 0, 1+len(values.Profiles))
	names = append(names, RouteProfileDefault)
	for name := range values.Profiles {
		name = strings.TrimSpace(name)
		if name == "" || name == RouteProfileDefault {
			continue
		}
		names = append(names, name)
	}
	if len(names) > 1 {
		sort.Strings(names[1:])
	}
	out := make([]ResolvedProfile, 0, len(names))
	for _, name := range names {
		profile, err := ResolveProfile(values, name)
		if err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, nil
}

func ResolveRouteWithProfileOverride(values RuntimeValues, purpose string, profileName string) (ResolvedRoute, error) {
	purpose = normalizeRoutePurpose(purpose)
	if !isSupportedRoutePurpose(purpose) {
		return ResolvedRoute{}, fmt.Errorf("unsupported llm route purpose %q", strings.TrimSpace(purpose))
	}
	if values.Routes.ParseErr != nil {
		return ResolvedRoute{}, values.Routes.ParseErr
	}
	policy := resolveRoutePolicy(values.Routes, purpose)
	if err := validateRoutePolicy(policy, purpose); err != nil {
		return ResolvedRoute{}, err
	}
	profile, err := ResolveProfile(values, profileName)
	if err != nil {
		return ResolvedRoute{}, err
	}
	fallbacks, err := resolveFallbacks(values, policy.FallbackProfiles, []string{profile.Name})
	if err != nil {
		return ResolvedRoute{}, err
	}
	overridePolicy := policy
	overridePolicy.Profile = profile.Name
	overridePolicy.Candidates = nil
	return ResolvedRoute{
		Purpose:      purpose,
		Identity:     routePolicyIdentity(overridePolicy),
		Profile:      profile.Name,
		Values:       profile.Values,
		ClientConfig: profile.ClientConfig,
		Fallbacks:    fallbacks,
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
	raw := map[string]any{}
	if err := unmarshalKey(r, "llm.routes", &raw); err != nil || len(raw) == 0 {
		return RoutesConfig{}
	}
	routes, err := parseRoutesConfig(raw)
	if err != nil {
		return RoutesConfig{ParseErr: err}
	}
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
	cfg.Headers = cloneStringMap(cfg.Headers)
	cfg.CacheTTL = strings.TrimSpace(cfg.CacheTTL)
	cfg.CacheKeyPrefix = strings.TrimSpace(cfg.CacheKeyPrefix)
	cfg.RequestTimeoutRaw = strings.TrimSpace(cfg.RequestTimeoutRaw)
	cfg.ToolsEmulationMode = strings.TrimSpace(cfg.ToolsEmulationMode)
	cfg.TemperatureRaw = strings.TrimSpace(cfg.TemperatureRaw)
	cfg.ReasoningEffortRaw = strings.TrimSpace(cfg.ReasoningEffortRaw)
	cfg.ReasoningBudgetRaw = strings.TrimSpace(cfg.ReasoningBudgetRaw)
	cfg.Azure.Deployment = strings.TrimSpace(cfg.Azure.Deployment)
	cfg.Bedrock.AWSKey = strings.TrimSpace(cfg.Bedrock.AWSKey)
	cfg.Bedrock.AWSSecret = strings.TrimSpace(cfg.Bedrock.AWSSecret)
	cfg.Bedrock.AWSSessionToken = strings.TrimSpace(cfg.Bedrock.AWSSessionToken)
	cfg.Bedrock.AWSProfile = strings.TrimSpace(cfg.Bedrock.AWSProfile)
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
	cfg.MainLoop = normalizeRoutePolicy(cfg.MainLoop)
	cfg.Addressing = normalizeRoutePolicy(cfg.Addressing)
	cfg.Heartbeat = normalizeRoutePolicy(cfg.Heartbeat)
	cfg.PlanCreate = normalizeRoutePolicy(cfg.PlanCreate)
	cfg.MemoryDraft = normalizeRoutePolicy(cfg.MemoryDraft)
	return cfg
}

func normalizeRoutePolicy(cfg RoutePolicyConfig) RoutePolicyConfig {
	cfg.Profile = strings.TrimSpace(cfg.Profile)
	cfg.FallbackProfiles = normalizeProfileNames(cfg.FallbackProfiles)
	if len(cfg.Candidates) == 0 {
		cfg.Candidates = nil
		return cfg
	}
	out := make([]RouteCandidateConfig, 0, len(cfg.Candidates))
	for _, candidate := range cfg.Candidates {
		candidate.Profile = strings.TrimSpace(candidate.Profile)
		out = append(out, candidate)
	}
	cfg.Candidates = out
	return cfg
}

func resolveRoutePolicy(routes RoutesConfig, purpose string) RoutePolicyConfig {
	return routeTargetForPurpose(routes.PurposeRoutes, purpose)
}

func routeTargetForPurpose(routes PurposeRoutes, purpose string) RoutePolicyConfig {
	switch purpose {
	case RoutePurposeMainLoop:
		return routes.MainLoop
	case RoutePurposeAddressing:
		return routes.Addressing
	case RoutePurposeHeartbeat:
		return routes.Heartbeat
	case RoutePurposePlanCreate:
		return routes.PlanCreate
	case RoutePurposeMemoryDraft:
		return routes.MemoryDraft
	default:
		return RoutePolicyConfig{}
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
	out.Headers = mergeStringMaps(out.Headers, override.Headers)
	applyStringOverride(&out.CacheTTL, override.CacheTTL)
	applyStringOverride(&out.CacheKeyPrefix, override.CacheKeyPrefix)
	applyStringOverride(&out.RequestTimeoutRaw, override.RequestTimeoutRaw)
	applyStringOverride(&out.ToolsEmulationMode, override.ToolsEmulationMode)
	applyStringOverride(&out.TemperatureRaw, override.TemperatureRaw)
	applyStringOverride(&out.ReasoningEffortRaw, override.ReasoningEffortRaw)
	applyStringOverride(&out.ReasoningBudgetRaw, override.ReasoningBudgetRaw)
	applyStringOverride(&out.AzureDeployment, override.Azure.Deployment)
	applyStringOverride(&out.BedrockAWSKey, override.Bedrock.AWSKey)
	applyStringOverride(&out.BedrockAWSSecret, override.Bedrock.AWSSecret)
	applyStringOverride(&out.BedrockAWSSessionToken, override.Bedrock.AWSSessionToken)
	applyStringOverride(&out.BedrockAWSProfile, override.Bedrock.AWSProfile)
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

func resolveProfileValues(values RuntimeValues, profileName string) (RuntimeValues, error) {
	resolvedValues := cloneRuntimeValuesForRoute(values)
	if profileName == "" || profileName == RouteProfileDefault {
		return resolvedValues, nil
	}
	override, ok := values.Profiles[profileName]
	if !ok {
		return RuntimeValues{}, fmt.Errorf("missing profile %q", profileName)
	}
	return applyProfileOverride(resolvedValues, override), nil
}

func resolvedClientConfig(values RuntimeValues) (llmconfig.ClientConfig, error) {
	requestTimeout, err := requestTimeoutFromValue(values.RequestTimeoutRaw, "llm.request_timeout")
	if err != nil {
		return llmconfig.ClientConfig{}, err
	}
	provider := normalizeProvider(values.Provider)
	return llmconfig.ClientConfig{
		Provider:       provider,
		Endpoint:       EndpointForProviderWithValues(provider, values),
		APIKey:         APIKeyForProviderWithValues(provider, values),
		Model:          ModelForProviderWithValues(provider, values),
		Headers:        cloneStringMap(values.Headers),
		RequestTimeout: requestTimeout,
	}, nil
}

func mergeStringMaps(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := cloneStringMap(base)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range cloneStringMap(override) {
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolveFallbacks(values RuntimeValues, names []string, excludedProfiles []string) ([]ResolvedFallback, error) {
	names = normalizeProfileNames(names)
	if len(names) == 0 {
		return nil, nil
	}
	excluded := make(map[string]struct{}, len(excludedProfiles))
	for _, profile := range normalizeProfileNames(excludedProfiles) {
		excluded[profile] = struct{}{}
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]ResolvedFallback, 0, len(names))
	for _, name := range names {
		if _, skip := excluded[name]; skip {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		resolvedValues, err := resolveProfileValues(values, name)
		if err != nil {
			return nil, err
		}
		cfg, err := resolvedClientConfig(resolvedValues)
		if err != nil {
			return nil, err
		}
		out = append(out, ResolvedFallback{
			Profile:      name,
			Values:       resolvedValues,
			ClientConfig: cfg,
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func resolveRouteCandidates(values RuntimeValues, cfgs []RouteCandidateConfig, purpose string) ([]ResolvedCandidate, error) {
	if len(cfgs) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(cfgs))
	out := make([]ResolvedCandidate, 0, len(cfgs))
	for idx, candidate := range cfgs {
		profileName := strings.TrimSpace(candidate.Profile)
		if profileName == "" {
			return nil, fmt.Errorf("llm.routes.%s.candidates[%d].profile is required", purpose, idx)
		}
		if candidate.Weight <= 0 {
			return nil, fmt.Errorf("llm.routes.%s.candidates[%d].weight must be > 0", purpose, idx)
		}
		if _, ok := seen[profileName]; ok {
			return nil, fmt.Errorf("llm.routes.%s.candidates[%d].profile %q is duplicated", purpose, idx, profileName)
		}
		seen[profileName] = struct{}{}
		resolvedValues, err := resolveProfileValues(values, profileName)
		if err != nil {
			return nil, err
		}
		cfg, err := resolvedClientConfig(resolvedValues)
		if err != nil {
			return nil, err
		}
		out = append(out, ResolvedCandidate{
			Profile:      profileName,
			Values:       resolvedValues,
			ClientConfig: cfg,
			Weight:       candidate.Weight,
		})
	}
	return out, nil
}

func normalizeProfileNames(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validateRoutePolicy(policy RoutePolicyConfig, purpose string) error {
	if strings.TrimSpace(policy.Profile) != "" && len(policy.Candidates) > 0 {
		return fmt.Errorf("llm.routes.%s cannot set both profile and candidates", purpose)
	}
	return nil
}

func candidateProfiles(candidates []ResolvedCandidate) []string {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if profile := strings.TrimSpace(candidate.Profile); profile != "" {
			out = append(out, profile)
		}
	}
	return out
}

func displayCandidate(candidates []ResolvedCandidate) ResolvedCandidate {
	if len(candidates) == 0 {
		return ResolvedCandidate{}
	}
	for _, candidate := range candidates {
		if candidate.Profile == RouteProfileDefault {
			return candidate
		}
	}
	return candidates[0]
}

func routePolicyIdentity(policy RoutePolicyConfig) string {
	parts := make([]string, 0, 1+len(policy.Candidates)+len(policy.FallbackProfiles))
	if profile := strings.TrimSpace(policy.Profile); profile != "" {
		parts = append(parts, "profile="+profile)
	}
	if len(policy.Candidates) > 0 {
		candidateParts := make([]string, 0, len(policy.Candidates))
		for _, candidate := range policy.Candidates {
			candidateParts = append(candidateParts, strings.TrimSpace(candidate.Profile)+"="+strconv.Itoa(candidate.Weight))
		}
		parts = append(parts, "candidates="+strings.Join(candidateParts, ","))
	}
	if len(policy.FallbackProfiles) > 0 {
		parts = append(parts, "fallbacks="+strings.Join(policy.FallbackProfiles, ","))
	}
	if len(parts) == 0 {
		return "profile=" + RouteProfileDefault
	}
	return strings.Join(parts, "|")
}

func parseRoutesConfig(raw map[string]any) (RoutesConfig, error) {
	mainLoop, err := parseRoutePolicyValue(raw[RoutePurposeMainLoop], "llm.routes."+RoutePurposeMainLoop)
	if err != nil {
		return RoutesConfig{}, err
	}
	addressing, err := parseRoutePolicyValue(raw[RoutePurposeAddressing], "llm.routes."+RoutePurposeAddressing)
	if err != nil {
		return RoutesConfig{}, err
	}
	heartbeat, err := parseRoutePolicyValue(raw[RoutePurposeHeartbeat], "llm.routes."+RoutePurposeHeartbeat)
	if err != nil {
		return RoutesConfig{}, err
	}
	planCreate, err := parseRoutePolicyValue(raw[RoutePurposePlanCreate], "llm.routes."+RoutePurposePlanCreate)
	if err != nil {
		return RoutesConfig{}, err
	}
	memoryDraft, err := parseRoutePolicyValue(raw[RoutePurposeMemoryDraft], "llm.routes."+RoutePurposeMemoryDraft)
	if err != nil {
		return RoutesConfig{}, err
	}
	return RoutesConfig{
		PurposeRoutes: PurposeRoutes{
			MainLoop:    mainLoop,
			Addressing:  addressing,
			Heartbeat:   heartbeat,
			PlanCreate:  planCreate,
			MemoryDraft: memoryDraft,
		},
	}, nil
}

func parseRoutePolicyValue(raw any, path string) (RoutePolicyConfig, error) {
	switch value := raw.(type) {
	case nil:
		return RoutePolicyConfig{}, nil
	case string:
		return RoutePolicyConfig{Profile: strings.TrimSpace(value)}, nil
	case map[string]any:
		return parseRoutePolicyMap(value, path)
	case map[any]any:
		return parseRoutePolicyMap(normalizeStringAnyMap(value), path)
	default:
		return RoutePolicyConfig{}, fmt.Errorf("%s must be a string or object", path)
	}
}

func parseRoutePolicyMap(raw map[string]any, path string) (RoutePolicyConfig, error) {
	profile, err := stringValue(raw["profile"], path+".profile")
	if err != nil {
		return RoutePolicyConfig{}, err
	}
	fallbacks, err := stringSliceValue(raw["fallback_profiles"], path+".fallback_profiles")
	if err != nil {
		return RoutePolicyConfig{}, err
	}
	candidates, err := candidateConfigsValue(raw["candidates"], path+".candidates")
	if err != nil {
		return RoutePolicyConfig{}, err
	}
	return RoutePolicyConfig{
		Profile:          profile,
		Candidates:       candidates,
		FallbackProfiles: fallbacks,
	}, nil
}

func candidateConfigsValue(raw any, path string) ([]RouteCandidateConfig, error) {
	if raw == nil {
		return nil, nil
	}
	var items []any
	switch value := raw.(type) {
	case []any:
		items = value
	case []map[string]any:
		items = make([]any, 0, len(value))
		for _, item := range value {
			items = append(items, item)
		}
	case []map[any]any:
		items = make([]any, 0, len(value))
		for _, item := range value {
			items = append(items, item)
		}
	default:
		return nil, fmt.Errorf("%s must be a list", path)
	}
	out := make([]RouteCandidateConfig, 0, len(items))
	for idx, item := range items {
		var m map[string]any
		switch value := item.(type) {
		case map[string]any:
			m = value
		case map[any]any:
			m = normalizeStringAnyMap(value)
		default:
			return nil, fmt.Errorf("%s[%d] must be an object", path, idx)
		}
		profile, err := stringValue(m["profile"], fmt.Sprintf("%s[%d].profile", path, idx))
		if err != nil {
			return nil, err
		}
		weight, err := intValue(m["weight"])
		if err != nil {
			return nil, fmt.Errorf("%s[%d].weight is invalid", path, idx)
		}
		out = append(out, RouteCandidateConfig{
			Profile: profile,
			Weight:  weight,
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func stringValue(raw any, path string) (string, error) {
	switch value := raw.(type) {
	case nil:
		return "", nil
	case string:
		return strings.TrimSpace(value), nil
	default:
		return "", fmt.Errorf("%s must be a string", path)
	}
}

func stringSliceValue(raw any, path string) ([]string, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case []string:
		return normalizeProfileNames(value), nil
	case []any:
		out := make([]string, 0, len(value))
		for idx, item := range value {
			s, err := stringValue(item, fmt.Sprintf("%s[%d]", path, idx))
			if err != nil {
				return nil, err
			}
			if s != "" {
				out = append(out, s)
			}
		}
		return normalizeProfileNames(out), nil
	default:
		return nil, fmt.Errorf("%s must be a list", path)
	}
}

func intValue(raw any) (int, error) {
	switch value := raw.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		return int(value), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(value))
	default:
		return 0, fmt.Errorf("invalid int value")
	}
}

func normalizeStringAnyMap(raw map[any]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		k, ok := key.(string)
		if !ok {
			continue
		}
		out[k] = value
	}
	return out
}
