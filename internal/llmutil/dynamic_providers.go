package llmutil

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"gopkg.in/yaml.v3"
)

const dynamicLLMOverlayConfigPath = "file_state_dir/internal/llm_overlay.yaml"

func loadDynamicLLMProviders(values RuntimeValues) RuntimeValues {
	path := pathutil.ResolveStateFile(values.FileStateDir, "internal/llm_overlay.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values
		}
		return valuesWithRouteParseErr(values, fmt.Errorf("read %s: %w", dynamicLLMOverlayConfigPath, err))
	}
	expanded, missing := configutil.ExpandStrictEnv(string(raw))
	if len(missing) > 0 {
		return valuesWithRouteParseErr(
			values,
			fmt.Errorf("%s: unset environment variable(s): %s", dynamicLLMOverlayConfigPath, strings.Join(missing, ", ")),
		)
	}
	next, err := parseDynamicLLMProviders(values, []byte(expanded))
	if err != nil {
		return valuesWithRouteParseErr(values, err)
	}
	return next
}

func parseDynamicLLMProviders(values RuntimeValues, raw []byte) (RuntimeValues, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return RuntimeValues{}, fmt.Errorf("%s: %w", dynamicLLMOverlayConfigPath, err)
	}
	if len(doc) == 0 {
		return values, nil
	}
	if len(doc) != 1 {
		return RuntimeValues{}, fmt.Errorf("%s: top-level key must be llm_overlay", dynamicLLMOverlayConfigPath)
	}
	rawOverlay, ok := doc["llm_overlay"]
	if !ok {
		return RuntimeValues{}, fmt.Errorf("%s: top-level key must be llm_overlay", dynamicLLMOverlayConfigPath)
	}
	overlay, ok := rawOverlay.(map[string]any)
	if !ok {
		return RuntimeValues{}, fmt.Errorf("%s.llm_overlay must be an object", dynamicLLMOverlayConfigPath)
	}
	for key := range overlay {
		switch strings.TrimSpace(key) {
		case "providers", "default", RoutePurposeMainLoop:
		default:
			return RuntimeValues{}, fmt.Errorf("%s.llm_overlay: unsupported key %q", dynamicLLMOverlayConfigPath, key)
		}
	}
	if _, hasDefault := overlay["default"]; hasDefault {
		if _, hasMainLoop := overlay[RoutePurposeMainLoop]; hasMainLoop {
			return RuntimeValues{}, fmt.Errorf("%s.llm_overlay: default and main_loop cannot both be set", dynamicLLMOverlayConfigPath)
		}
	}

	profiles, err := dynamicProfileConfigs(values.Profiles, overlay["providers"])
	if err != nil {
		return RuntimeValues{}, err
	}
	if len(profiles) > 0 {
		if values.Profiles == nil {
			values.Profiles = map[string]ProfileConfig{}
		}
		for name, cfg := range profiles {
			values.Profiles[name] = cfg
		}
	}

	if routeRaw, ok := overlay["default"]; ok {
		policy, err := parseRoutePolicyValue(routeRaw, dynamicLLMOverlayConfigPath+".llm_overlay.default")
		if err != nil {
			return RuntimeValues{}, err
		}
		policy = normalizeRoutePolicy(policy)
		if err := validateDynamicRoutePolicyReferences(values, policy, "default"); err != nil {
			return RuntimeValues{}, err
		}
		values.Routes.MainLoop = policy
		return values, nil
	}
	if routeRaw, ok := overlay[RoutePurposeMainLoop]; ok {
		policy, err := parseRoutePolicyValue(routeRaw, dynamicLLMOverlayConfigPath+".llm_overlay."+RoutePurposeMainLoop)
		if err != nil {
			return RuntimeValues{}, err
		}
		policy = normalizeRoutePolicy(policy)
		if err := validateDynamicRoutePolicyReferences(values, policy, RoutePurposeMainLoop); err != nil {
			return RuntimeValues{}, err
		}
		values.Routes.MainLoop = policy
		return values, nil
	}
	if len(profiles) > 0 && routePolicyEmpty(values.Routes.MainLoop) {
		values.Routes.MainLoop = defaultDynamicRoutePolicy(profiles)
	}
	return values, nil
}

func dynamicProfileConfigs(existing map[string]ProfileConfig, raw any) (map[string]ProfileConfig, error) {
	if raw == nil {
		return nil, nil
	}
	providers, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.llm_overlay.providers must be an object", dynamicLLMOverlayConfigPath)
	}
	out := make(map[string]ProfileConfig, len(providers))
	for rawName, rawCfg := range providers {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, fmt.Errorf("%s.llm_overlay.providers contains an empty profile name", dynamicLLMOverlayConfigPath)
		}
		if name == RouteProfileDefault {
			return nil, fmt.Errorf("%s.llm_overlay.providers.%s is reserved", dynamicLLMOverlayConfigPath, RouteProfileDefault)
		}
		if _, ok := existing[name]; ok {
			return nil, fmt.Errorf("%s.llm_overlay.providers.%s conflicts with llm.profiles.%s", dynamicLLMOverlayConfigPath, name, name)
		}
		profileMap, ok := rawCfg.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.llm_overlay.providers.%s must be an object", dynamicLLMOverlayConfigPath, name)
		}
		if err := validateDynamicProfileKeys(name, profileMap); err != nil {
			return nil, err
		}
		cfg, err := dynamicProfileConfig(name, profileMap)
		if err != nil {
			return nil, err
		}
		out[name] = cfg
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validateDynamicProfileKeys(name string, raw map[string]any) error {
	allowed := map[string]struct{}{
		"inference_provider":      {},
		"provider":                {},
		"endpoint":                {},
		"api_key":                 {},
		"model":                   {},
		"context_window_tokens":   {},
		"headers":                 {},
		"cache_ttl":               {},
		"cache_key_prefix":        {},
		"request_timeout":         {},
		"tools_emulation_mode":    {},
		"temperature":             {},
		"reasoning_effort":        {},
		"reasoning_budget_tokens": {},
		"azure":                   {},
		"bedrock":                 {},
		"cloudflare":              {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("%s.llm_overlay.providers.%s.%s is not supported", dynamicLLMOverlayConfigPath, name, key)
		}
	}
	return nil
}

func dynamicProfileConfig(name string, raw map[string]any) (ProfileConfig, error) {
	b, err := yaml.Marshal(raw)
	if err != nil {
		return ProfileConfig{}, err
	}
	var cfg ProfileConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return ProfileConfig{}, fmt.Errorf("%s.llm_overlay.providers.%s: %w", dynamicLLMOverlayConfigPath, name, err)
	}
	cfg = normalizeProfileConfig(cfg)
	cfg.Source = ProfileSourceState
	cfg.NoInheritIdentity = true
	values := applyProfileOverride(RuntimeValues{}, cfg)
	values, err = ResolveRuntimeValuesInferenceProvider(values)
	if err != nil {
		return ProfileConfig{}, fmt.Errorf("%s.llm_overlay.providers.%s: %w", dynamicLLMOverlayConfigPath, name, err)
	}
	if strings.TrimSpace(values.Provider) == "" {
		return ProfileConfig{}, fmt.Errorf("%s.llm_overlay.providers.%s must set provider or inference_provider", dynamicLLMOverlayConfigPath, name)
	}
	if info, ok := InferenceProviderInfoByValue(cfg.InferenceProvider); ok && info.RequiresAPIBase && strings.TrimSpace(values.Endpoint) == "" {
		return ProfileConfig{}, fmt.Errorf("%s.llm_overlay.providers.%s.endpoint is required for %s", dynamicLLMOverlayConfigPath, name, cfg.InferenceProvider)
	}
	return cfg, nil
}

func validateDynamicRoutePolicyReferences(values RuntimeValues, policy RoutePolicyConfig, key string) error {
	for _, profile := range referencedRouteProfiles(policy) {
		if profile == RouteProfileDefault {
			continue
		}
		if _, ok := values.Profiles[profile]; !ok {
			return fmt.Errorf("%s.llm_overlay.%s references missing profile %q", dynamicLLMOverlayConfigPath, key, profile)
		}
	}
	return validateRoutePolicy(policy, RoutePurposeMainLoop)
}

func referencedRouteProfiles(policy RoutePolicyConfig) []string {
	var out []string
	if profile := strings.TrimSpace(policy.Profile); profile != "" {
		out = append(out, profile)
	}
	for _, candidate := range policy.Candidates {
		if profile := strings.TrimSpace(candidate.Profile); profile != "" {
			out = append(out, profile)
		}
	}
	out = append(out, policy.FallbackProfiles...)
	return normalizeProfileNames(out)
}

func defaultDynamicRoutePolicy(profiles map[string]ProfileConfig) RoutePolicyConfig {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	candidates := make([]RouteCandidateConfig, 0, 1+len(names))
	candidates = append(candidates, RouteCandidateConfig{Profile: RouteProfileDefault, Weight: 1})
	for _, name := range names {
		candidates = append(candidates, RouteCandidateConfig{Profile: name, Weight: 1})
	}
	return RoutePolicyConfig{Candidates: candidates}
}

func valuesWithRouteParseErr(values RuntimeValues, err error) RuntimeValues {
	if values.Routes.ParseErr == nil {
		values.Routes.ParseErr = err
	}
	return values
}
