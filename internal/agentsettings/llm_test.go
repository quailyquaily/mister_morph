package agentsettings

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

func TestSettingsPayloadFromRuntimeValuesExposesCurrentMainLoopProfile(t *testing.T) {
	got := SettingsPayloadFromRuntimeValues(llmutil.RuntimeValues{
		Provider: "openai",
		Model:    "gpt-default",
		Profiles: map[string]llmutil.ProfileConfig{
			"cheap": {Model: "gpt-cheap"},
		},
		Routes: llmutil.RoutesConfig{
			PurposeRoutes: llmutil.PurposeRoutes{
				MainLoop: llmutil.RoutePolicyConfig{Profile: "cheap"},
			},
		},
	})

	if got.CurrentProfile != "cheap" {
		t.Fatalf("CurrentProfile = %q, want cheap", got.CurrentProfile)
	}
}

func TestSettingsPayloadFromRuntimeValuesProfileDoesNotUseDefaultProvider(t *testing.T) {
	got := SettingsPayloadFromRuntimeValues(llmutil.RuntimeValues{
		InferenceProvider: llmutil.InferenceProviderXAIOAuth,
		Provider:          "xai_oauth",
		Model:             "grok-4.5",
		Profiles: map[string]llmutil.ProfileConfig{
			"standalone": {
				Endpoint: "https://profile.example.test/v1",
				APIKey:   "profile-key",
				Model:    "profile-model",
			},
		},
	})

	if len(got.Profiles) != 1 {
		t.Fatalf("profiles = %#v, want one", got.Profiles)
	}
	profile := got.Profiles[0]
	if profile.Endpoint != "https://profile.example.test/v1" || profile.APIKey != "profile-key" {
		t.Fatalf("profile = %+v, must not be sanitized as top-level xai_oauth", profile)
	}
}

func TestResolveOpenAICompatibleModelLookup_DerivesBuiltInEndpoint(t *testing.T) {
	got, err := ResolveOpenAICompatibleModelLookup(
		LLMSettingsPayload{},
		ModelLookupRequest{
			InferenceProvider: llmutil.InferenceProviderGroq,
			APIKey:            "sk-test",
		},
		nil,
	)
	if err != nil {
		t.Fatalf("ResolveOpenAICompatibleModelLookup() error = %v", err)
	}
	if got.Endpoint != llmutil.DefaultGroqEndpoint {
		t.Fatalf("endpoint = %q, want %q", got.Endpoint, llmutil.DefaultGroqEndpoint)
	}
	if got.APIKey != "sk-test" {
		t.Fatalf("api key = %q, want sk-test", got.APIKey)
	}
}

func TestResolveOpenAICompatibleModelLookup_ExplicitEndpointOverridesCurrentInferenceProvider(t *testing.T) {
	got, err := ResolveOpenAICompatibleModelLookup(
		LLMSettingsPayload{
			LLMConfigFieldsPayload: LLMConfigFieldsPayload{
				InferenceProvider: llmutil.InferenceProviderOpenAI,
				Provider:          "openai_resp",
				Endpoint:          llmutil.DefaultOpenAIEndpoint,
				APIKey:            "sk-current",
			},
		},
		ModelLookupRequest{
			Endpoint: "https://models.example.test",
			APIKey:   "sk-request",
		},
		nil,
	)
	if err != nil {
		t.Fatalf("ResolveOpenAICompatibleModelLookup() error = %v", err)
	}
	if got.Endpoint != "https://models.example.test" {
		t.Fatalf("endpoint = %q, want explicit endpoint", got.Endpoint)
	}
	if got.APIKey != "sk-request" {
		t.Fatalf("api key = %q, want request key", got.APIKey)
	}
}

func TestResolveOpenAICompatibleModelLookup_ExplicitRouteDoesNotUseCurrentConnectionFields(t *testing.T) {
	current := LLMSettingsPayload{LLMConfigFieldsPayload: LLMConfigFieldsPayload{
		InferenceProvider: llmutil.InferenceProviderOpenAI,
		Provider:          "openai_resp",
		Endpoint:          llmutil.DefaultOpenAIEndpoint,
		APIKey:            "sk-current",
	}}

	t.Run("api base", func(t *testing.T) {
		_, err := ResolveOpenAICompatibleModelLookup(
			current,
			ModelLookupRequest{InferenceProvider: llmutil.InferenceProviderOpenAIResponseCompatible},
			nil,
		)
		if err == nil || err.Error() != "api base is required" {
			t.Fatalf("error = %v, want api base is required", err)
		}
	})

	t.Run("api key", func(t *testing.T) {
		_, err := ResolveOpenAICompatibleModelLookup(
			current,
			ModelLookupRequest{InferenceProvider: llmutil.InferenceProviderOpenAI},
			nil,
		)
		if err == nil || err.Error() != "api key is required" {
			t.Fatalf("error = %v, want api key is required", err)
		}
	})
}

func TestSanitizeProviderSpecificLLMFieldsClearsCredentialsForXAIOAuth(t *testing.T) {
	got := SanitizeProviderSpecificLLMFields(LLMConfigFieldsPayload{
		InferenceProvider:   llmutil.InferenceProviderXAIOAuth,
		Provider:            "xai_oauth",
		Endpoint:            "https://attacker.example.test/v1",
		APIKey:              "api-secret",
		BedrockAWSKey:       "aws-key",
		BedrockAWSSecret:    "aws-secret",
		BedrockRegion:       "region",
		BedrockModelARN:     "arn",
		CloudflareAPIToken:  "cf-secret",
		CloudflareAccountID: "cf-account",
	}, "xai_oauth")
	if got.Endpoint != "" || got.APIKey != "" || got.BedrockAWSKey != "" ||
		got.BedrockAWSSecret != "" || got.BedrockRegion != "" || got.BedrockModelARN != "" ||
		got.CloudflareAPIToken != "" || got.CloudflareAccountID != "" {
		t.Fatalf("sanitized fields = %+v", got)
	}
	if got.InferenceProvider != llmutil.InferenceProviderXAIOAuth || got.Provider != "xai_oauth" {
		t.Fatalf("provider identity was changed: %+v", got)
	}
	if got := ResolvedAgentSettingsAPIKey("xai_oauth", "api-secret"); got != "" {
		t.Fatalf("ResolvedAgentSettingsAPIKey() = %q, want empty", got)
	}
	if got := ResolvedCloudflareToken("xai_oauth", "api-secret", "cf-secret"); got != "" {
		t.Fatalf("ResolvedCloudflareToken() = %q, want empty", got)
	}
	if got := ResolvedCloudflareAccountID("xai_oauth", "cf-account"); got != "" {
		t.Fatalf("ResolvedCloudflareAccountID() = %q, want empty", got)
	}
}

func TestSanitizeProviderSpecificLLMFieldsPreservesCodexAPIKey(t *testing.T) {
	got := SanitizeProviderSpecificLLMFields(LLMConfigFieldsPayload{
		InferenceProvider:   llmutil.InferenceProviderOpenAICodex,
		Provider:            "openai_codex",
		Endpoint:            "https://codex.example.test/api",
		APIKey:              "provider-key",
		BedrockAWSKey:       "aws-key",
		CloudflareAPIToken:  "cf-secret",
		CloudflareAccountID: "cf-account",
	}, "openai_codex")

	if got.Endpoint != "https://codex.example.test/api" || got.APIKey != "provider-key" {
		t.Fatalf("Codex endpoint/API key = %q/%q", got.Endpoint, got.APIKey)
	}
	if got.BedrockAWSKey != "" || got.CloudflareAPIToken != "" || got.CloudflareAccountID != "" {
		t.Fatalf("unrelated credentials were not cleared: %+v", got)
	}
	if key := ResolvedAgentSettingsAPIKey("openai_codex", "provider-key"); key != "provider-key" {
		t.Fatalf("ResolvedAgentSettingsAPIKey() = %q, want provider-key", key)
	}
}
