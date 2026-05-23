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
