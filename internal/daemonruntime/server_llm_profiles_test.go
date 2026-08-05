package daemonruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
)

func TestLLMProfilesRouteReturnsNamedProfiles(t *testing.T) {
	settings := viper.New()
	settings.Set("llm.inference_provider", "openai")
	settings.Set("llm.model", "gpt-5.2")
	settings.Set("llm.routes.main_loop.profile", "cheap")
	settings.Set("llm.profiles", map[string]any{
		"local": map[string]any{
			"inference_provider": "ollama",
			"model":              "qwen3:8b",
		},
		"cheap": map[string]any{
			"inference_provider": "openai",
			"model":              "gpt-4.1-mini",
		},
	})

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:                "console",
		AuthToken:           "token",
		AgentSettingsReader: settings,
	})

	req := httptest.NewRequest(http.MethodGet, "/llm/profiles", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Default struct {
			Name              string `json:"name"`
			InferenceProvider string `json:"inference_provider"`
			Model             string `json:"model"`
		} `json:"default"`
		Items []struct {
			Name              string `json:"name"`
			InferenceProvider string `json:"inference_provider"`
			Model             string `json:"model"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := payload.Default; got.Name != "default" || got.InferenceProvider != "openai" || got.Model != "gpt-4.1-mini" {
		t.Fatalf("default = %#v, want main_loop route using openai/gpt-4.1-mini", got)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items = %#v, want two named profiles", payload.Items)
	}
	if got := payload.Items[0]; got.Name != "cheap" || got.InferenceProvider != "openai" || got.Model != "gpt-4.1-mini" {
		t.Fatalf("items[0] = %#v, want cheap profile", got)
	}
	if got := payload.Items[1]; got.Name != "local" || got.InferenceProvider != "ollama" || got.Model != "qwen3:8b" {
		t.Fatalf("items[1] = %#v, want local profile", got)
	}
}

func TestLLMProfilesRouteRequiresAuthentication(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{AuthToken: "token"})

	req := httptest.NewRequest(http.MethodGet, "/llm/profiles", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRegisterRoutesCapturesAgentSettingsReaderOnce(t *testing.T) {
	settings := viper.New()
	settings.Set("llm.profiles", map[string]any{
		"captured": map[string]any{"model": "model-a"},
	})

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken:           "token",
		AgentSettingsReader: settings,
	})
	settings.Set("llm.profiles", map[string]any{
		"late": map[string]any{"model": "model-b"},
	})

	req := httptest.NewRequest(http.MethodGet, "/llm/profiles", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Items []runtimeLLMProfileOption `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Name != "captured" {
		t.Fatalf("items = %#v, want captured settings", payload.Items)
	}
}
