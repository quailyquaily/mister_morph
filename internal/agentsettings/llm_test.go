package agentsettings

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

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
