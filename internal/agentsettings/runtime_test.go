package agentsettings

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/spf13/viper"
)

func TestNormalizeOpenAICompatibleModelsURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "default", in: "", want: "https://api.openai.com/v1/models"},
		{name: "base", in: "https://example.test", want: "https://example.test/v1/models"},
		{name: "v1", in: "https://example.test/v1/", want: "https://example.test/v1/models"},
		{name: "models", in: "https://example.test/v1/models", want: "https://example.test/v1/models"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeOpenAICompatibleModelsURL(tc.in)
			if err != nil {
				t.Fatalf("NormalizeOpenAICompatibleModelsURL() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("URL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEffectiveRuntimeValuesUsesOnlyPassedReader(t *testing.T) {
	reader := viper.New()
	reader.Set("llm.provider", "openai")
	reader.Set("llm.model", "reader-model")
	reader.Set("file_state_dir", t.TempDir())

	values, err := EffectiveRuntimeValues(reader)
	if err != nil {
		t.Fatalf("EffectiveRuntimeValues() error = %v", err)
	}
	if values.Model != "reader-model" {
		t.Fatalf("model = %q, want reader-model", values.Model)
	}
	if values.FileStateDir != reader.GetString("file_state_dir") {
		t.Fatalf("file state dir = %q, want passed reader value %q", values.FileStateDir, reader.GetString("file_state_dir"))
	}
}

func TestResolveConnectionTestValuesPreservesReaderRuntimeFields(t *testing.T) {
	reader := viper.New()
	reader.Set("llm.provider", "openai")
	reader.Set("llm.model", "base-model")
	reader.Set("llm.request_timeout", "17s")
	reader.Set("llm.headers", map[string]string{"X-Test": "reader"})
	reader.Set("file_state_dir", t.TempDir())

	values, err := ResolveConnectionTestValues(
		reader,
		LLMSettingsPayload{LLMConfigFieldsPayload: LLMConfigFieldsPayload{Provider: "openai", Model: "test-model"}},
		llmutil.RouteProfileDefault,
		configutil.SecretRefSourceFromReader(reader),
	)
	if err != nil {
		t.Fatalf("ResolveConnectionTestValues() error = %v", err)
	}
	if values.Model != "test-model" {
		t.Fatalf("model = %q, want test-model", values.Model)
	}
	if values.RequestTimeoutRaw != "17s" {
		t.Fatalf("request timeout = %q, want 17s", values.RequestTimeoutRaw)
	}
	if values.Headers["X-Test"] != "reader" {
		t.Fatalf("headers = %#v, want reader header", values.Headers)
	}
	if values.FileStateDir != reader.GetString("file_state_dir") {
		t.Fatalf("file state dir = %q, want passed reader value %q", values.FileStateDir, reader.GetString("file_state_dir"))
	}
}

func TestResolveConnectionTestValuesNamedProfileIgnoresDefaultLLMFields(t *testing.T) {
	t.Setenv("PROFILE_API_KEY", "profile-key")
	reader := viper.New()
	reader.Set("llm.inference_provider", llmutil.InferenceProviderAnthropic)
	reader.Set("llm.provider", "anthropic")
	reader.Set("llm.endpoint", llmutil.DefaultAnthropicEndpoint)
	reader.Set("llm.api_key", "reader-key")
	reader.Set("llm.model", "reader-model")
	reader.Set("llm.request_timeout", "17s")
	reader.Set("llm.headers", map[string]string{"X-Test": "reader"})
	reader.Set("llm.cache_ttl", "long")
	reader.Set("llm.cache_key_prefix", "reader-cache")
	reader.Set("llm.temperature", "0.8")
	reader.Set("llm.pricing_file", "./pricing.yaml")
	reader.Set("llm.image.provider", "gemini")
	reader.Set("llm.image.model", "gemini-image")
	reader.Set("config", "/config/config.yaml")
	reader.Set("file_state_dir", "/state")

	values, err := ResolveConnectionTestValues(
		reader,
		LLMSettingsPayload{
			LLMConfigFieldsPayload: LLMConfigFieldsPayload{
				InferenceProvider: llmutil.InferenceProviderAnthropic,
				Provider:          "anthropic",
				Endpoint:          llmutil.DefaultAnthropicEndpoint,
				APIKey:            "${MISSING_DEFAULT_API_KEY}",
				Model:             "draft-default-model",
			},
			Profiles: []LLMProfileSettingsPayload{{
				Name: "isolated",
				LLMConfigFieldsPayload: LLMConfigFieldsPayload{
					InferenceProvider: llmutil.InferenceProviderOpenAIChatCompatible,
					Endpoint:          "https://profile.example.test/v1",
					APIKey:            "${PROFILE_API_KEY}",
					Model:             "profile-model",
				},
			}},
		},
		"isolated",
		configutil.SecretRefSourceFromReader(reader),
	)
	if err != nil {
		t.Fatalf("ResolveConnectionTestValues() error = %v", err)
	}
	if values.InferenceProvider != llmutil.InferenceProviderOpenAIChatCompatible || values.Provider != "openai" {
		t.Fatalf("provider = %q/%q, want profile provider", values.InferenceProvider, values.Provider)
	}
	if values.Endpoint != "https://profile.example.test/v1" || values.APIKey != "profile-key" || values.Model != "profile-model" {
		t.Fatalf("profile connection fields = endpoint %q, key %q, model %q", values.Endpoint, values.APIKey, values.Model)
	}
	if values.RequestTimeoutRaw != "" || len(values.Headers) != 0 || values.CacheTTL != "" ||
		values.CacheKeyPrefix != "" || values.TemperatureRaw != "" {
		t.Fatalf("named profile inherited default LLM fields: %+v", values)
	}
	if values.PricingFile != "./pricing.yaml" || values.ConfigPath != "/config/config.yaml" || values.FileStateDir != "/state" {
		t.Fatalf("shared runtime paths = %+v", values)
	}
	if values.ImageProvider != "gemini" || values.ImageModel != "gemini-image" {
		t.Fatalf("shared image config = %+v", values)
	}
}

func TestCurrentLLMEnvManagedFieldsRedactsSecrets(t *testing.T) {
	t.Setenv("MISTER_MORPH_LLM_PROVIDER", "openai")
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "secret")

	fields := CurrentLLMEnvManagedFields("openai")
	if got := fields["provider"].Value; got != "openai" {
		t.Fatalf("provider value = %q, want openai", got)
	}
	apiKey, ok := fields["api_key"]
	if !ok {
		t.Fatal("api_key env field is missing")
	}
	if apiKey.Value != "" {
		t.Fatalf("api_key value = %q, want redacted", apiKey.Value)
	}
	if apiKey.EnvName != "MISTER_MORPH_LLM_API_KEY" || apiKey.RawValue != "${MISTER_MORPH_LLM_API_KEY}" {
		t.Fatalf("api_key metadata = %#v", apiKey)
	}
}

func TestCurrentLLMEnvManagedFieldsIgnoresConnectionFieldsForXAIOAuth(t *testing.T) {
	t.Setenv("MISTER_MORPH_LLM_ENDPOINT", "https://attacker.example.test/v1")
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "secret")
	t.Setenv("MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN", "cf-secret")
	t.Setenv("MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID", "cf-account")

	fields := CurrentLLMEnvManagedFields("xai_oauth")
	for _, key := range []string{"endpoint", "api_key", "cloudflare_api_token", "cloudflare_account_id"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("%s must not be env-managed for xai_oauth: %#v", key, fields[key])
		}
	}
}

func TestCurrentLLMEnvManagedFieldsAllowsCodexEndpointOverride(t *testing.T) {
	t.Setenv("MISTER_MORPH_LLM_ENDPOINT", "https://codex.example.test/api")
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "provider-key")

	fields := CurrentLLMEnvManagedFields("openai_codex")
	endpoint, ok := fields["endpoint"]
	if !ok || endpoint.Value != "https://codex.example.test/api" {
		t.Fatalf("endpoint metadata = %#v, want Codex endpoint override", endpoint)
	}
	apiKey, ok := fields["api_key"]
	if !ok || apiKey.EnvName != "MISTER_MORPH_LLM_API_KEY" || apiKey.RawValue != "${MISTER_MORPH_LLM_API_KEY}" {
		t.Fatalf("api_key metadata = %#v, want Codex API key override", apiKey)
	}
	if apiKey.Value != "" {
		t.Fatalf("api_key value = %q, want redacted", apiKey.Value)
	}
}

func TestNewReaderSnapshotDoesNotObserveSourceMutation(t *testing.T) {
	source := viper.New()
	source.Set("llm.model", "captured-model")
	source.Set("llm.profiles", map[string]any{
		"captured": map[string]any{"model": "profile-a"},
	})

	snapshot := NewReaderSnapshot(source)
	source.Set("llm.model", "mutated-model")
	source.Set("llm.profiles", map[string]any{
		"mutated": map[string]any{"model": "profile-b"},
	})

	values, err := llmutil.RuntimeValuesFromReader(snapshot)
	if err != nil {
		t.Fatalf("RuntimeValuesFromReader() error = %v", err)
	}
	if values.Model != "captured-model" {
		t.Fatalf("model = %q, want captured-model", values.Model)
	}
	if _, ok := values.Profiles["captured"]; !ok {
		t.Fatalf("profiles = %#v, want captured profile", values.Profiles)
	}
	if _, ok := values.Profiles["mutated"]; ok {
		t.Fatalf("profiles = %#v, must not observe source mutation", values.Profiles)
	}
}
