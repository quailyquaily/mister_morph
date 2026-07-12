package daemonruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/spf13/viper"
)

type fakeRuntimeAgentSettingsSecretRefSource struct {
	secrets map[string]string
	errs    map[string]error
}

func (f fakeRuntimeAgentSettingsSecretRefSource) LookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

func (f fakeRuntimeAgentSettingsSecretRefSource) GetAWSSecretString(_ context.Context, secretID string) (string, error) {
	if err := f.errs[secretID]; err != nil {
		return "", err
	}
	value, ok := f.secrets[secretID]
	if !ok {
		return "", secref.ErrAWSSecretNotFound
	}
	return value, nil
}

func TestAgentSettingsRouteReturnsReadOnlyRuntimeSettings(t *testing.T) {
	clearRuntimeAgentSettingsEnv(t)
	t.Setenv("MISTER_MORPH_LLM_API_KEY", "env-key")

	stateDir := t.TempDir()
	reader := viper.New()
	reader.Set("config", filepath.Join(stateDir, "config.yaml"))
	reader.Set("file_state_dir", stateDir)
	reader.Set("llm.provider", "openai")
	reader.Set("llm.endpoint", "https://api.example.test/v1")
	reader.Set("llm.model", "gpt-test")
	reader.Set("llm.api_key", "config-key")
	reader.Set("llm.reasoning_effort", "medium")
	reader.Set("llm.tools_emulation_mode", "fallback")
	reader.Set("tools.write_file.enabled", true)
	reader.Set("tools.coder.enabled", true)
	reader.Set("tools.bash.enabled", true)
	reader.Set("tools.powershell.enabled", false)

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken:            "token",
		AgentSettingsEnabled: true,
		AgentSettingsReader: func() *viper.Viper {
			return reader
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/settings/agent", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		ReadOnly bool `json:"read_only"`
		LLM      struct {
			Provider           string `json:"provider"`
			Endpoint           string `json:"endpoint"`
			Model              string `json:"model"`
			APIKey             string `json:"api_key"`
			ReasoningEffort    string `json:"reasoning_effort"`
			ToolsEmulationMode string `json:"tools_emulation_mode"`
		} `json:"llm"`
		EnvManaged struct {
			LLM map[string]struct {
				EnvName  string `json:"env_name"`
				Value    string `json:"value"`
				RawValue string `json:"raw_value"`
			} `json:"llm"`
		} `json:"env_managed"`
		Tools struct {
			WriteFile struct {
				Enabled bool `json:"enabled"`
			} `json:"write_file"`
			Coder struct {
				Enabled bool `json:"enabled"`
			} `json:"coder"`
			Bash struct {
				Enabled bool `json:"enabled"`
			} `json:"bash"`
			PowerShell struct {
				Enabled bool `json:"enabled"`
			} `json:"powershell"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !payload.ReadOnly {
		t.Fatal("read_only = false, want true")
	}
	if payload.LLM.Provider != "openai_custom" || payload.LLM.Endpoint != "https://api.example.test/v1" || payload.LLM.Model != "gpt-test" {
		t.Fatalf("unexpected llm payload: %#v", payload.LLM)
	}
	if payload.LLM.APIKey != "" {
		t.Fatalf("api_key = %q, want hidden env-managed secret", payload.LLM.APIKey)
	}
	field := payload.EnvManaged.LLM["api_key"]
	if field.EnvName != "MISTER_MORPH_LLM_API_KEY" || field.RawValue != "${MISTER_MORPH_LLM_API_KEY}" || field.Value != "" {
		t.Fatalf("unexpected env-managed api key field: %#v", field)
	}
	if !payload.Tools.WriteFile.Enabled || !payload.Tools.Coder.Enabled || !payload.Tools.Bash.Enabled || payload.Tools.PowerShell.Enabled {
		t.Fatalf("unexpected tools payload: %#v", payload.Tools)
	}
}

func TestAgentSettingsRouteRejectsWrites(t *testing.T) {
	clearRuntimeAgentSettingsEnv(t)

	reader := viper.New()
	reader.Set("file_state_dir", t.TempDir())
	reader.Set("llm.provider", "openai")
	reader.Set("llm.model", "gpt-test")

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken:            "token",
		AgentSettingsEnabled: true,
		AgentSettingsReader: func() *viper.Viper {
			return reader
		},
	})

	req := httptest.NewRequest(http.MethodPut, "/settings/agent", strings.NewReader(`{"llm":{"model":"next"}}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "read-only") {
		t.Fatalf("expected read-only error, got %q", rec.Body.String())
	}
}

func TestRuntimeAgentSettingsTestAcceptsGroqInferenceProvider(t *testing.T) {
	clearRuntimeAgentSettingsEnv(t)

	reader := viper.New()
	reader.Set("file_state_dir", t.TempDir())
	reader.Set("llm.inference_provider", "openai")
	reader.Set("llm.provider", "openai")
	reader.Set("llm.endpoint", "https://api.openai.com")
	reader.Set("llm.model", "gpt-5")
	reader.Set("llm.api_key", "sk-runtime")
	reader.Set("llm.request_timeout", "2m")

	settings, err := runtimeResolveAgentSettingsTestLLMFromReader(reader, runtimeAgentSettingsTestRequest{
		LLM: runtimeLLMSettingsPayload{
			LLMConfigFieldsPayload: runtimeLLMConfigFieldsPayload{
				InferenceProvider: llmutil.InferenceProviderGroq,
				Provider:          "openai_custom",
				Model:             "llama-3.3-70b-versatile",
				APIKey:            "sk-test",
			},
		},
	})
	if err != nil {
		t.Fatalf("resolve settings: %v", err)
	}
	if settings.InferenceProvider != llmutil.InferenceProviderGroq {
		t.Fatalf("inference_provider = %q, want %q", settings.InferenceProvider, llmutil.InferenceProviderGroq)
	}
	if settings.Provider != "openai_custom" {
		t.Fatalf("provider = %q, want openai_custom", settings.Provider)
	}
	if settings.Endpoint != llmutil.DefaultGroqEndpoint {
		t.Fatalf("endpoint = %q, want %s", settings.Endpoint, llmutil.DefaultGroqEndpoint)
	}

	values, err := runtimeValuesFromAgentSettingsTestLLM(reader, settings, secref.EnvSource{})
	if err != nil {
		t.Fatalf("runtime values: %v", err)
	}
	route, err := llmutil.ResolveRoute(values, llmutil.RoutePurposeMainLoop)
	if err != nil {
		t.Fatalf("resolve route: %v", err)
	}
	if route.ClientConfig.Provider != "openai_custom" {
		t.Fatalf("route provider = %q, want openai_custom", route.ClientConfig.Provider)
	}
	if route.ClientConfig.Endpoint != llmutil.DefaultGroqEndpoint {
		t.Fatalf("route endpoint = %q, want %s", route.ClientConfig.Endpoint, llmutil.DefaultGroqEndpoint)
	}
	if values.RequestTimeoutRaw != "2m" {
		t.Fatalf("request timeout = %q, want 2m", values.RequestTimeoutRaw)
	}
}

func TestRuntimeResolveAgentSettingsTestFieldValueWithSourceAWSSecretRef(t *testing.T) {
	src := fakeRuntimeAgentSettingsSecretRefSource{secrets: map[string]string{
		"mistermorph/openai-api-key": "sk-from-aws",
	}}

	got, err := runtimeResolveAgentSettingsTestFieldValueWithSource("${aws-sm:mistermorph/openai-api-key}", src)
	if err != nil {
		t.Fatalf("runtimeResolveAgentSettingsTestFieldValueWithSource() error = %v", err)
	}
	if got != "sk-from-aws" {
		t.Fatalf("resolved value = %q, want AWS secret", got)
	}
}

func TestRuntimeResolveAgentSettingsTestFieldValueWithSourceAWSFailureExpandsEmpty(t *testing.T) {
	src := fakeRuntimeAgentSettingsSecretRefSource{errs: map[string]error{
		"mistermorph/missing": fmt.Errorf("failed with sk-should-not-leak"),
	}}

	got, err := runtimeResolveAgentSettingsTestFieldValueWithSource("${aws-sm:mistermorph/missing}", src)
	if err != nil {
		t.Fatalf("runtimeResolveAgentSettingsTestFieldValueWithSource() error = %v", err)
	}
	if got != "" {
		t.Fatalf("resolved value = %q, want empty string", got)
	}
}

func clearRuntimeAgentSettingsEnv(t *testing.T) {
	t.Helper()
	names := []string{
		"MISTER_MORPH_LLM_PROVIDER",
		"MISTER_MORPH_LLM_ENDPOINT",
		"MISTER_MORPH_LLM_API_KEY",
		"MISTER_MORPH_LLM_MODEL",
		"MISTER_MORPH_LLM_AZURE_DEPLOYMENT",
		"MISTER_MORPH_LLM_REASONING_EFFORT",
		"MISTER_MORPH_LLM_TOOLS_EMULATION_MODE",
		"MISTER_MORPH_LLM_BEDROCK_AWS_KEY",
		"MISTER_MORPH_LLM_BEDROCK_AWS_SECRET",
		"MISTER_MORPH_LLM_BEDROCK_REGION",
		"MISTER_MORPH_LLM_BEDROCK_MODEL_ARN",
		"MISTER_MORPH_LLM_CLOUDFLARE_ACCOUNT_ID",
		"MISTER_MORPH_LLM_CLOUDFLARE_API_TOKEN",
	}
	previous := make(map[string]string, len(names))
	present := make(map[string]bool, len(names))
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			previous[name] = value
			present[name] = true
		}
		_ = os.Unsetenv(name)
	}
	t.Cleanup(func() {
		for _, name := range names {
			if present[name] {
				_ = os.Setenv(name, previous[name])
			} else {
				_ = os.Unsetenv(name)
			}
		}
	})
}
