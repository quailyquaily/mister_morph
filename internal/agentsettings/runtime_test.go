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
