package llmutil

import (
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
				MainLoop: "cheap",
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
		RequestTimeoutRaw: "90s",
		Profiles: map[string]ProfileConfig{
			"cheap": {
				Model:          "gpt-4.1-mini",
				TemperatureRaw: "0.2",
			},
		},
		Routes: RoutesConfig{
			PurposeRoutes: PurposeRoutes{
				Addressing: "cheap",
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
				MemoryDraft: "memory",
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
				PlanCreate: "reasoning",
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
				PlanCreate: "reasoning",
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

func TestRuntimeValuesFromReader_LoadProfilesAndRoutes(t *testing.T) {
	v := viper.New()
	v.Set("llm.provider", "openai")
	v.Set("llm.endpoint", "https://api.openai.com")
	v.Set("llm.api_key", "base-key")
	v.Set("llm.model", "gpt-5.2")
	v.Set("llm.request_timeout", "90s")
	v.Set("llm.profiles", map[string]any{
		"cheap": map[string]any{
			"model":       "gpt-4.1-mini",
			"temperature": "0.2",
		},
		"reasoning": map[string]any{
			"provider":         "xai",
			"model":            "grok-4.1-fast-reasoning",
			"api_key":          "xai-key",
			"reasoning_effort": "high",
		},
	})
	v.Set("llm.routes", map[string]any{
		"main_loop":    "default",
		"addressing":   "cheap",
		"plan_create":  "reasoning",
		"memory_draft": "cheap",
	})

	values := RuntimeValuesFromReader(v)
	if values.Profiles["cheap"].Model != "gpt-4.1-mini" {
		t.Fatalf("cheap model = %q, want gpt-4.1-mini", values.Profiles["cheap"].Model)
	}
	if values.Profiles["reasoning"].ReasoningEffortRaw != "high" {
		t.Fatalf("reasoning effort = %q, want high", values.Profiles["reasoning"].ReasoningEffortRaw)
	}
	if values.Profiles["reasoning"].APIKey != "xai-key" {
		t.Fatalf("reasoning api key = %q, want xai-key", values.Profiles["reasoning"].APIKey)
	}
	if values.Routes.Addressing != "cheap" {
		t.Fatalf("addressing route = %q, want cheap", values.Routes.Addressing)
	}
	if values.Routes.MainLoop != "default" {
		t.Fatalf("main loop route = %q, want default", values.Routes.MainLoop)
	}
	if values.Routes.MemoryDraft != "cheap" {
		t.Fatalf("memory draft route = %q, want cheap", values.Routes.MemoryDraft)
	}
}
