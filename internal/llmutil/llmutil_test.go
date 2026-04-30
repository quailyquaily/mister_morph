package llmutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/spf13/viper"
)

func TestEndpointForProviderWithValues_CloudflareDefaultEndpoint(t *testing.T) {
	values := RuntimeValues{Endpoint: "https://api.openai.com"}
	if got := EndpointForProviderWithValues("cloudflare", values); got != "" {
		t.Fatalf("EndpointForProviderWithValues() = %q, want empty", got)
	}
	values.Endpoint = "https://api.openai.com/v1"
	if got := EndpointForProviderWithValues("cloudflare", values); got != "" {
		t.Fatalf("EndpointForProviderWithValues() = %q, want empty", got)
	}
}

func TestEndpointForProviderWithValues_CloudflareCustomEndpoint(t *testing.T) {
	values := RuntimeValues{Endpoint: "https://gateway.ai.cloudflare.com/v1/acc/route"}
	if got := EndpointForProviderWithValues("cloudflare", values); got != values.Endpoint {
		t.Fatalf("EndpointForProviderWithValues() = %q, want %q", got, values.Endpoint)
	}
}

func TestAPIKeyForProviderWithValues_CloudflareFallback(t *testing.T) {
	values := RuntimeValues{
		APIKey:             "generic-key",
		CloudflareAPIToken: "cf-token",
	}
	if got := APIKeyForProviderWithValues("cloudflare", values); got != "cf-token" {
		t.Fatalf("APIKeyForProviderWithValues() = %q, want cf-token", got)
	}
	values.CloudflareAPIToken = ""
	if got := APIKeyForProviderWithValues("cloudflare", values); got != "generic-key" {
		t.Fatalf("APIKeyForProviderWithValues() = %q, want generic-key", got)
	}
}

func TestRuntimeValuesFromReader_ReadsMisterMorphLLMAPIKeyFromEnv(t *testing.T) {
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "env-llm-key")

	v := viper.New()
	v.SetEnvPrefix("MISTER_MORPH")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()

	values := RuntimeValuesFromReader(v)
	if values.APIKey != "env-llm-key" {
		t.Fatalf("RuntimeValuesFromReader().APIKey = %q, want env-llm-key", values.APIKey)
	}

	resolved, err := ResolveRoute(values, RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	if resolved.ClientConfig.APIKey != "env-llm-key" {
		t.Fatalf("resolved api key = %q, want env-llm-key", resolved.ClientConfig.APIKey)
	}
}

func TestRuntimeValuesFromReader_UsesEnvWhenConfigOmitsLLMAPIKey(t *testing.T) {
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "env-llm-key")

	v := viper.New()
	v.SetEnvPrefix("MISTER_MORPH")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("llm:\n  provider: openai\n  model: gpt-5.2\n")); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	values := RuntimeValuesFromReader(v)
	if values.APIKey != "env-llm-key" {
		t.Fatalf("RuntimeValuesFromReader().APIKey = %q, want env-llm-key", values.APIKey)
	}
}

func TestRuntimeValuesFromReader_ReadsLLMHeaders(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(`
llm:
  provider: openai_resp
  headers:
    "-X-ABC-TOKEN": "${ABC_TOKEN}"
    OpenAI-Organization: org_123
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}

	values := RuntimeValuesFromReader(v)
	if got := values.Headers["-x-abc-token"]; got != "${ABC_TOKEN}" {
		t.Fatalf("headers[-x-abc-token] = %q, want ${ABC_TOKEN}", got)
	}
	if got := values.Headers["openai-organization"]; got != "org_123" {
		t.Fatalf("headers[openai-organization] = %q, want org_123", got)
	}
}

func TestModelForProviderWithValues_AzureDeploymentFirst(t *testing.T) {
	values := RuntimeValues{
		Model:           "gpt-5.2",
		AzureDeployment: "gpt5-deploy",
	}
	if got := ModelForProviderWithValues("azure", values); got != "gpt5-deploy" {
		t.Fatalf("ModelForProviderWithValues() = %q, want gpt5-deploy", got)
	}
	values.AzureDeployment = ""
	if got := ModelForProviderWithValues("azure", values); got != "gpt-5.2" {
		t.Fatalf("ModelForProviderWithValues() = %q, want gpt-5.2", got)
	}
}

func TestClientFromConfigWithValues_InvalidToolsMode(t *testing.T) {
	_, err := ClientFromConfigWithValues(llmconfig.ClientConfig{
		Provider:       "openai",
		Endpoint:       "https://api.openai.com",
		APIKey:         "k",
		Model:          "gpt-5.2",
		RequestTimeout: 10 * time.Second,
	}, RuntimeValues{
		ToolsEmulationMode: "invalid",
	})
	if err == nil {
		t.Fatalf("expected error for invalid tools emulation mode")
	}
	if !strings.Contains(err.Error(), "llm.tools_emulation_mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientFromConfigWithValues_InvalidTemperature(t *testing.T) {
	_, err := ClientFromConfigWithValues(llmconfig.ClientConfig{
		Provider: "openai",
	}, RuntimeValues{
		TemperatureRaw: "abc",
	})
	if err == nil {
		t.Fatalf("expected error for invalid temperature")
	}
	if !strings.Contains(err.Error(), "llm.temperature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientFromConfigWithValues_InvalidReasoningEffort(t *testing.T) {
	_, err := ClientFromConfigWithValues(llmconfig.ClientConfig{
		Provider: "openai",
	}, RuntimeValues{
		ReasoningEffortRaw: "extreme",
	})
	if err == nil {
		t.Fatalf("expected error for invalid reasoning effort")
	}
	if !strings.Contains(err.Error(), "llm.reasoning_effort") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientFromConfigWithValues_InvalidReasoningBudget(t *testing.T) {
	_, err := ClientFromConfigWithValues(llmconfig.ClientConfig{
		Provider: "openai",
	}, RuntimeValues{
		ReasoningBudgetRaw: "8k",
	})
	if err == nil {
		t.Fatalf("expected error for invalid reasoning budget")
	}
	if !strings.Contains(err.Error(), "llm.reasoning_budget_tokens") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPricingCatalogFromValues_ResolvesRelativeToConfigPath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	pricingPath := filepath.Join(dir, "pricing.yaml")
	if err := os.WriteFile(pricingPath, []byte("version: uniai.pricing.v1\nchat:\n  - inference_provider: openai\n    model: gpt-5.4\n    input_usd_per_million: 1\n    output_usd_per_million: 2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(pricing.yaml) error = %v", err)
	}

	pricing, digest, err := LoadPricingCatalog(RuntimeValues{
		ConfigPath:  configPath,
		PricingFile: "./pricing.yaml",
	})
	if err != nil {
		t.Fatalf("LoadPricingCatalog() error = %v", err)
	}
	if pricing == nil || len(pricing.Chat) != 1 {
		t.Fatalf("pricing catalog = %#v, want one chat rule", pricing)
	}
	if strings.TrimSpace(digest) == "" {
		t.Fatalf("expected non-empty pricing digest")
	}
	if pricing.Chat[0].InferenceProvider != "openai" || pricing.Chat[0].Model != "gpt-5.4" {
		t.Fatalf("pricing rule = %#v", pricing.Chat[0])
	}
}

func TestPricingCatalogFromValues_MissingFileFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()

	pricing, digest, err := LoadPricingCatalog(RuntimeValues{
		ConfigPath:  filepath.Join(dir, "config.yaml"),
		PricingFile: "./pricing.yaml",
	})
	if err != nil {
		t.Fatalf("LoadPricingCatalog() error = %v", err)
	}
	if pricing == nil {
		t.Fatalf("expected default pricing catalog")
	}
	if len(pricing.Chat) == 0 {
		t.Fatalf("expected default pricing catalog to include chat rules")
	}
	if strings.TrimSpace(digest) == "" {
		t.Fatalf("expected non-empty pricing digest")
	}
}

func TestPricingCatalogFromValues_EmptyPathFallsBackToDefault(t *testing.T) {
	pricing, digest, err := LoadPricingCatalog(RuntimeValues{})
	if err != nil {
		t.Fatalf("LoadPricingCatalog() error = %v", err)
	}
	if pricing == nil {
		t.Fatalf("expected default pricing catalog")
	}
	if len(pricing.Chat) == 0 {
		t.Fatalf("expected default pricing catalog to include chat rules")
	}
	if strings.TrimSpace(digest) == "" {
		t.Fatalf("expected non-empty pricing digest")
	}
}

func TestPricingCatalogFromValues_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	pricingPath := filepath.Join(dir, "pricing.yaml")
	if err := os.WriteFile(pricingPath, []byte("version: ["), 0o644); err != nil {
		t.Fatalf("WriteFile(pricing.yaml) error = %v", err)
	}

	_, _, err := LoadPricingCatalog(RuntimeValues{
		ConfigPath:  configPath,
		PricingFile: "./pricing.yaml",
	})
	if err == nil {
		t.Fatalf("expected parse error for invalid pricing yaml")
	}
	if !strings.Contains(err.Error(), "llm.pricing_file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRoute_DefaultMainLoop(t *testing.T) {
	values := RuntimeValues{
		Provider:          "openai",
		Endpoint:          "https://api.openai.com",
		APIKey:            "base-key",
		Model:             "gpt-5.2",
		RequestTimeoutRaw: "90s",
	}
	resolved, err := ResolveRoute(values, RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	if resolved.Profile != RouteProfileDefault {
		t.Fatalf("profile = %q, want default", resolved.Profile)
	}
	if resolved.ClientConfig.Model != "gpt-5.2" {
		t.Fatalf("model = %q, want gpt-5.2", resolved.ClientConfig.Model)
	}
	if resolved.ClientConfig.RequestTimeout != 90*time.Second {
		t.Fatalf("request timeout = %v, want 90s", resolved.ClientConfig.RequestTimeout)
	}
}

func TestResolveRoute_GlobalPurposeOverride(t *testing.T) {
	values := RuntimeValues{
		Provider:          "openai",
		Endpoint:          "https://api.openai.com",
		APIKey:            "base-key",
		Model:             "gpt-5.2",
		RequestTimeoutRaw: "90s",
		Profiles: map[string]ProfileConfig{
			"cheap": {
				Model: "gpt-4.1-mini",
			},
		},
		Routes: RoutesConfig{
			PurposeRoutes: PurposeRoutes{
				MainLoop: RoutePolicyConfig{Profile: "cheap"},
			},
		},
	}
	resolved, err := ResolveRoute(values, RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	if resolved.Profile != "cheap" {
		t.Fatalf("profile = %q, want cheap", resolved.Profile)
	}
	if resolved.ClientConfig.Model != "gpt-4.1-mini" {
		t.Fatalf("model = %q, want gpt-4.1-mini", resolved.ClientConfig.Model)
	}
}

func TestResolveRoute_ProfileInheritance(t *testing.T) {
	values := RuntimeValues{
		Provider:          "openai",
		Endpoint:          "https://api.openai.com",
		APIKey:            "base-key",
		Model:             "gpt-5.2",
		Headers:           map[string]string{"X-App-Name": "mistermorph", "X-Trace": "base"},
		RequestTimeoutRaw: "90s",
		Profiles: map[string]ProfileConfig{
			"cheap": {
				Model:          "gpt-4.1-mini",
				Headers:        map[string]string{"X-Trace": "cheap", "-X-ABC-TOKEN": "p1"},
				TemperatureRaw: "0.2",
			},
		},
		Routes: RoutesConfig{
			PurposeRoutes: PurposeRoutes{
				Addressing: RoutePolicyConfig{Profile: "cheap"},
			},
		},
	}
	resolved, err := ResolveRoute(values, RoutePurposeAddressing)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	if resolved.ClientConfig.APIKey != "base-key" {
		t.Fatalf("api key = %q, want base-key", resolved.ClientConfig.APIKey)
	}
	if resolved.Values.TemperatureRaw != "0.2" {
		t.Fatalf("temperature raw = %q, want 0.2", resolved.Values.TemperatureRaw)
	}
	if got := resolved.ClientConfig.Headers["X-App-Name"]; got != "mistermorph" {
		t.Fatalf("headers[X-App-Name] = %q, want mistermorph", got)
	}
	if got := resolved.ClientConfig.Headers["X-Trace"]; got != "cheap" {
		t.Fatalf("headers[X-Trace] = %q, want cheap", got)
	}
	if got := resolved.ClientConfig.Headers["-X-ABC-TOKEN"]; got != "p1" {
		t.Fatalf("headers[-X-ABC-TOKEN] = %q, want p1", got)
	}
	if len(resolved.Fallbacks) != 0 {
		t.Fatalf("fallbacks = %d, want 0", len(resolved.Fallbacks))
	}
}

func TestResolveRoute_MemoryDraftPurpose(t *testing.T) {
	values := RuntimeValues{
		Provider:          "openai",
		Endpoint:          "https://api.openai.com",
		APIKey:            "base-key",
		Model:             "gpt-5.2",
		RequestTimeoutRaw: "90s",
		Profiles: map[string]ProfileConfig{
			"memory": {
				Model: "gpt-4.1-mini",
			},
		},
		Routes: RoutesConfig{
			PurposeRoutes: PurposeRoutes{
				MemoryDraft: RoutePolicyConfig{Profile: "memory"},
			},
		},
	}
	resolved, err := ResolveRoute(values, RoutePurposeMemoryDraft)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	if resolved.Profile != "memory" {
		t.Fatalf("profile = %q, want memory", resolved.Profile)
	}
	if resolved.ClientConfig.Model != "gpt-4.1-mini" {
		t.Fatalf("model = %q, want gpt-4.1-mini", resolved.ClientConfig.Model)
	}
}

func TestResolveRoute_RouteLocalFallbackProfiles(t *testing.T) {
	values := RuntimeValues{
		Provider:          "openai",
		Endpoint:          "https://api.openai.com",
		APIKey:            "base-key",
		Model:             "gpt-5.2",
		RequestTimeoutRaw: "90s",
		Profiles: map[string]ProfileConfig{
			"cheap": {
				Model: "gpt-4.1-mini",
			},
			"reasoning": {
				Provider: "xai",
				Model:    "grok-4.1-fast-reasoning",
				APIKey:   "xai-key",
			},
		},
		Routes: RoutesConfig{
			PurposeRoutes: PurposeRoutes{
				MainLoop: RoutePolicyConfig{
					FallbackProfiles: []string{"cheap", "reasoning", "cheap"},
				},
			},
		},
	}
	resolved, err := ResolveRoute(values, RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	if got := len(resolved.Fallbacks); got != 2 {
		t.Fatalf("fallback count = %d, want 2", got)
	}
	if resolved.Fallbacks[0].Profile != "cheap" {
		t.Fatalf("fallback[0].profile = %q, want cheap", resolved.Fallbacks[0].Profile)
	}
	if resolved.Fallbacks[0].ClientConfig.Model != "gpt-4.1-mini" {
		t.Fatalf("fallback[0].model = %q, want gpt-4.1-mini", resolved.Fallbacks[0].ClientConfig.Model)
	}
	if resolved.Fallbacks[1].Profile != "reasoning" {
		t.Fatalf("fallback[1].profile = %q, want reasoning", resolved.Fallbacks[1].Profile)
	}
	if resolved.Fallbacks[1].ClientConfig.Provider != "xai" {
		t.Fatalf("fallback[1].provider = %q, want xai", resolved.Fallbacks[1].ClientConfig.Provider)
	}
}

func TestResolveRoute_ProfileAPIKeyOverride(t *testing.T) {
	values := RuntimeValues{
		Provider: "openai",
		APIKey:   "base-key",
		Model:    "gpt-5.2",
		Profiles: map[string]ProfileConfig{
			"reasoning": {
				Provider: "xai",
				Model:    "grok-4.1-fast-reasoning",
				APIKey:   "xai-key",
			},
		},
		Routes: RoutesConfig{
			PurposeRoutes: PurposeRoutes{
				PlanCreate: RoutePolicyConfig{Profile: "reasoning"},
			},
		},
	}
	resolved, err := ResolveRoute(values, RoutePurposePlanCreate)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	if resolved.ClientConfig.Provider != "xai" {
		t.Fatalf("provider = %q, want xai", resolved.ClientConfig.Provider)
	}
	if resolved.ClientConfig.APIKey != "xai-key" {
		t.Fatalf("api key = %q, want xai-key", resolved.ClientConfig.APIKey)
	}
}

func TestResolveRoute_CloudflareAPIToken(t *testing.T) {
	values := RuntimeValues{
		Provider:            "cloudflare",
		Model:               "@cf/meta/llama-4",
		CloudflareAccountID: "acc-id",
		CloudflareAPIToken:  "cf-token",
	}
	resolved, err := ResolveRoute(values, RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	if resolved.ClientConfig.Provider != "cloudflare" {
		t.Fatalf("provider = %q, want cloudflare", resolved.ClientConfig.Provider)
	}
	if resolved.ClientConfig.APIKey != "cf-token" {
		t.Fatalf("api key = %q, want cf-token", resolved.ClientConfig.APIKey)
	}
}

func TestResolveRoute_MissingProfile(t *testing.T) {
	values := RuntimeValues{
		Provider: "openai",
		Model:    "gpt-5.2",
		Routes: RoutesConfig{
			PurposeRoutes: PurposeRoutes{
				PlanCreate: RoutePolicyConfig{Profile: "reasoning"},
			},
		},
	}
	_, err := ResolveRoute(values, RoutePurposePlanCreate)
	if err == nil {
		t.Fatalf("expected missing profile error")
	}
	if !strings.Contains(err.Error(), "missing profile") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRoute_InvalidRouteFallbackProfile(t *testing.T) {
	values := RuntimeValues{
		Provider: "openai",
		Model:    "gpt-5.2",
		Routes: RoutesConfig{
			PurposeRoutes: PurposeRoutes{
				MainLoop: RoutePolicyConfig{
					FallbackProfiles: []string{"missing"},
				},
			},
		},
	}
	_, err := ResolveRoute(values, RoutePurposeMainLoop)
	if err == nil {
		t.Fatalf("expected invalid fallback profile error")
	}
	if !strings.Contains(err.Error(), `missing profile "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRoute_FallbackDedupesPrimaryAndCandidates(t *testing.T) {
	values := RuntimeValues{
		Provider: "openai",
		Model:    "gpt-5.2",
		Profiles: map[string]ProfileConfig{
			"cheap": {
				Model: "gpt-4.1-mini",
			},
			"reasoning": {
				Model: "grok-4.1-fast-reasoning",
			},
		},
		Routes: RoutesConfig{
			PurposeRoutes: PurposeRoutes{
				MainLoop: RoutePolicyConfig{
					Candidates: []RouteCandidateConfig{
						{Profile: "default", Weight: 1},
						{Profile: "cheap", Weight: 1},
					},
					FallbackProfiles: []string{"default", "cheap", "reasoning"},
				},
			},
		},
	}
	resolved, err := ResolveRoute(values, RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("ResolveRoute() error = %v", err)
	}
	if got := len(resolved.Fallbacks); got != 1 {
		t.Fatalf("fallback count = %d, want 1", got)
	}
	if resolved.Fallbacks[0].Profile != "reasoning" {
		t.Fatalf("fallback[0].profile = %q, want reasoning", resolved.Fallbacks[0].Profile)
	}
}

func TestRuntimeValuesFromReader_LoadProfilesAndRoutes(t *testing.T) {
	v := viper.New()
	v.Set("llm.provider", "openai")
	v.Set("llm.endpoint", "https://api.openai.com")
	v.Set("llm.api_key", "base-key")
	v.Set("llm.model", "gpt-5.2")
	v.Set("llm.cache_ttl", "short")
	v.Set("llm.cache_key_prefix", "base-cache")
	v.Set("llm.request_timeout", "90s")
	v.Set("llm.profiles", map[string]any{
		"cheap": map[string]any{
			"model":            "gpt-4.1-mini",
			"temperature":      "0.2",
			"cache_ttl":        "long",
			"cache_key_prefix": "cheap-cache",
		},
		"reasoning": map[string]any{
			"provider":         "xai",
			"model":            "grok-4.1-fast-reasoning",
			"api_key":          "xai-key",
			"reasoning_effort": "high",
		},
	})
	v.Set("llm.routes", map[string]any{
		"main_loop": map[string]any{
			"candidates": []map[string]any{
				{"profile": "default", "weight": 1},
				{"profile": "cheap", "weight": 1},
			},
			"fallback_profiles": []string{"reasoning"},
		},
		"addressing":   "cheap",
		"plan_create":  "reasoning",
		"memory_draft": map[string]any{"profile": "cheap"},
	})

	values := RuntimeValuesFromReader(v)
	if values.Profiles["cheap"].Model != "gpt-4.1-mini" {
		t.Fatalf("cheap model = %q, want gpt-4.1-mini", values.Profiles["cheap"].Model)
	}
	if values.CacheTTL != "short" {
		t.Fatalf("cache_ttl = %q, want short", values.CacheTTL)
	}
	if values.CacheKeyPrefix != "base-cache" {
		t.Fatalf("cache_key_prefix = %q, want base-cache", values.CacheKeyPrefix)
	}
	if values.Profiles["cheap"].CacheTTL != "long" {
		t.Fatalf("cheap cache_ttl = %q, want long", values.Profiles["cheap"].CacheTTL)
	}
	if values.Profiles["cheap"].CacheKeyPrefix != "cheap-cache" {
		t.Fatalf("cheap cache_key_prefix = %q, want cheap-cache", values.Profiles["cheap"].CacheKeyPrefix)
	}
	if values.Profiles["reasoning"].ReasoningEffortRaw != "high" {
		t.Fatalf("reasoning effort = %q, want high", values.Profiles["reasoning"].ReasoningEffortRaw)
	}
	if values.Profiles["reasoning"].APIKey != "xai-key" {
		t.Fatalf("reasoning api key = %q, want xai-key", values.Profiles["reasoning"].APIKey)
	}
	if values.Routes.Addressing.Profile != "cheap" {
		t.Fatalf("addressing route profile = %q, want cheap", values.Routes.Addressing.Profile)
	}
	if len(values.Routes.MainLoop.Candidates) != 2 {
		t.Fatalf("main loop candidate count = %d, want 2", len(values.Routes.MainLoop.Candidates))
	}
	if values.Routes.MainLoop.FallbackProfiles[0] != "reasoning" {
		t.Fatalf("main loop fallback = %#v, want [reasoning]", values.Routes.MainLoop.FallbackProfiles)
	}
	if values.Routes.MemoryDraft.Profile != "cheap" {
		t.Fatalf("memory draft route profile = %q, want cheap", values.Routes.MemoryDraft.Profile)
	}
}

func TestResolveProfile_AppliesCacheTTLOverrides(t *testing.T) {
	values := RuntimeValues{
		Provider:       "openai_resp",
		Model:          "gpt-5.2",
		CacheTTL:       "short",
		CacheKeyPrefix: "base-cache",
		Profiles: map[string]ProfileConfig{
			"cheap": {
				Model:          "gpt-4.1-mini",
				CacheTTL:       "long",
				CacheKeyPrefix: "cheap-cache",
			},
		},
	}

	resolved, err := ResolveProfile(values, "cheap")
	if err != nil {
		t.Fatalf("ResolveProfile() error = %v", err)
	}
	if resolved.Values.CacheTTL != "long" {
		t.Fatalf("resolved cache_ttl = %q, want long", resolved.Values.CacheTTL)
	}
	if resolved.Values.CacheKeyPrefix != "cheap-cache" {
		t.Fatalf("resolved cache_key_prefix = %q, want cheap-cache", resolved.Values.CacheKeyPrefix)
	}
	if resolved.ClientConfig.Model != "gpt-4.1-mini" {
		t.Fatalf("resolved model = %q, want gpt-4.1-mini", resolved.ClientConfig.Model)
	}
}

func TestSystemPromptCacheControl(t *testing.T) {
	ctrl, err := SystemPromptCacheControl("short")
	if err != nil {
		t.Fatalf("SystemPromptCacheControl() error = %v", err)
	}
	if ctrl == nil || ctrl.TTL != "short" {
		t.Fatalf("cache control = %#v, want TTL short", ctrl)
	}
}

func TestSystemPromptCacheControlEmpty(t *testing.T) {
	ctrl, err := SystemPromptCacheControl("")
	if err != nil {
		t.Fatalf("SystemPromptCacheControl() error = %v", err)
	}
	if ctrl != nil {
		t.Fatalf("cache control = %#v, want nil", ctrl)
	}
}

func TestSystemPromptCacheControlOff(t *testing.T) {
	ctrl, err := SystemPromptCacheControl("off")
	if err != nil {
		t.Fatalf("SystemPromptCacheControl() error = %v", err)
	}
	if ctrl != nil {
		t.Fatalf("cache control = %#v, want nil", ctrl)
	}
}

func TestSystemPromptCacheControlRejectsInvalidTTL(t *testing.T) {
	ctrl, err := SystemPromptCacheControl("not-a-ttl")
	if err == nil {
		t.Fatal("expected error for invalid cache ttl")
	}
	if ctrl != nil {
		t.Fatalf("cache control = %#v, want nil", ctrl)
	}
	if !strings.Contains(err.Error(), "expected off|short|long|Go duration") {
		t.Fatalf("error = %v, want cache ttl validation message", err)
	}
}
