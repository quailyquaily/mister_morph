package daemonruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/spf13/viper"
)

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
		AgentSettingsReader:  reader,
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
	if payload.LLM.Provider != "openai" || payload.LLM.Endpoint != "https://api.example.test/v1" || payload.LLM.Model != "gpt-test" {
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
		AgentSettingsReader:  reader,
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

func TestAgentSettingsRouteUsesWritableOwner(t *testing.T) {
	clearRuntimeAgentSettingsEnv(t)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  provider: openai\n  model: before\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	reader := viper.New()
	reader.SetConfigFile(configPath)
	if err := reader.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig() error = %v", err)
	}
	reader.Set("config", configPath)
	owner := agentsettings.NewFileOwner(agentsettings.FileOwnerOptions{ConfigPath: configPath, Reader: reader})

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken:            "token",
		AgentSettingsEnabled: true,
		AgentSettingsOwner:   owner,
		AgentSettingsReader:  reader,
	})

	put := httptest.NewRequest(http.MethodPut, "/settings/agent", strings.NewReader(`{"llm":{"model":"after"}}`))
	put.Header.Set("Authorization", "Bearer token")
	putRecorder := httptest.NewRecorder()
	mux.ServeHTTP(putRecorder, put)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d (%s)", putRecorder.Code, http.StatusOK, putRecorder.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/settings/agent", nil)
	get.Header.Set("Authorization", "Bearer token")
	getRecorder := httptest.NewRecorder()
	mux.ServeHTTP(getRecorder, get)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (%s)", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
	var payload struct {
		ReadOnly bool `json:"read_only"`
		LLM      struct {
			Model string `json:"model"`
		} `json:"llm"`
	}
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if payload.ReadOnly || payload.LLM.Model != "after" {
		t.Fatalf("GET payload = %#v, want writable after model", payload)
	}
}

func TestAgentSettingsRouteUsesCurrentModelSelection(t *testing.T) {
	clearRuntimeAgentSettingsEnv(t)

	reader := viper.New()
	reader.SetConfigType("yaml")
	if err := reader.ReadConfig(strings.NewReader(`
llm:
  provider: openai
  model: gpt-default
  profiles:
    configured:
      model: gpt-configured
    manual:
      model: gpt-manual
  routes:
    main_loop:
      profile: configured
`)); err != nil {
		t.Fatalf("ReadConfig() error = %v", err)
	}
	reader.Set("file_state_dir", t.TempDir())

	selectionStore := llmselect.ProcessStore()
	previousSelection := selectionStore.Get()
	t.Cleanup(func() {
		if previousSelection.Mode == llmselect.ModeManual {
			selectionStore.SetProfile(previousSelection.ManualProfile)
		} else {
			selectionStore.Reset()
		}
	})

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken:            "token",
		AgentSettingsEnabled: true,
		AgentSettingsReader:  reader,
	})

	currentProfile := func() string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/settings/agent", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			LLM struct {
				CurrentProfile string `json:"current_profile"`
			} `json:"llm"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		return payload.LLM.CurrentProfile
	}

	selectionStore.SetProfile("manual")
	if got := currentProfile(); got != "manual" {
		t.Fatalf("current profile after manual selection = %q, want manual", got)
	}

	selectionStore.Reset()
	if got := currentProfile(); got != "configured" {
		t.Fatalf("current profile after reset = %q, want configured", got)
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
