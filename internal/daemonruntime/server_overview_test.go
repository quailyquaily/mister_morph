package daemonruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOverviewAddsVersionAndRuntimeWhenMissing(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
		Overview: func(context.Context) (map[string]any, error) {
			return map[string]any{
				"llm": map[string]any{"provider": "openai"},
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/overview", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	version, _ := payload["version"].(string)
	if strings.TrimSpace(version) == "" {
		t.Fatalf("expected non-empty version, got %v", payload["version"])
	}

	runtimePayload, ok := payload["runtime"].(map[string]any)
	if !ok || runtimePayload == nil {
		t.Fatalf("expected runtime object, got %T", payload["runtime"])
	}
	for _, key := range []string{"go_version", "goroutines", "heap_alloc_bytes", "heap_sys_bytes", "heap_objects", "gc_cycles"} {
		if _, exists := runtimePayload[key]; !exists {
			t.Fatalf("expected runtime.%s in payload", key)
		}
	}
}

func TestHealthAndOverviewResolveAgentNamePerRequest(t *testing.T) {
	currentName := "Morph"
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:          "serve",
		AuthToken:     "token",
		HealthEnabled: true,
		AgentNameFunc: func() string { return currentName },
	})

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	mux.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d (%s)", healthRec.Code, http.StatusOK, healthRec.Body.String())
	}
	var healthPayload map[string]any
	if err := json.Unmarshal(healthRec.Body.Bytes(), &healthPayload); err != nil {
		t.Fatalf("health json: %v", err)
	}
	if got, _ := healthPayload["agent_name"].(string); got != "Morph" {
		t.Fatalf("health agent_name = %q, want %q", got, "Morph")
	}

	currentName = "Momo"

	overviewReq := httptest.NewRequest(http.MethodGet, "/overview", nil)
	overviewReq.Header.Set("Authorization", "Bearer token")
	overviewRec := httptest.NewRecorder()
	mux.ServeHTTP(overviewRec, overviewReq)
	if overviewRec.Code != http.StatusOK {
		t.Fatalf("overview status = %d, want %d (%s)", overviewRec.Code, http.StatusOK, overviewRec.Body.String())
	}
	var overviewPayload map[string]any
	if err := json.Unmarshal(overviewRec.Body.Bytes(), &overviewPayload); err != nil {
		t.Fatalf("overview json: %v", err)
	}
	if got, _ := overviewPayload["agent_name"].(string); got != "Momo" {
		t.Fatalf("overview agent_name = %q, want %q", got, "Momo")
	}
}

func TestOverviewPreservesProvidedVersionAndRuntime(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
		Overview: func(context.Context) (map[string]any, error) {
			return map[string]any{
				"version": "custom-version",
				"runtime": map[string]any{
					"go_version": "custom-go",
				},
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/overview", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if got, _ := payload["version"].(string); got != "custom-version" {
		t.Fatalf("expected version custom-version, got %v", payload["version"])
	}

	runtimePayload, ok := payload["runtime"].(map[string]any)
	if !ok || runtimePayload == nil {
		t.Fatalf("expected runtime object, got %T", payload["runtime"])
	}
	if got, _ := runtimePayload["go_version"].(string); got != "custom-go" {
		t.Fatalf("expected runtime.go_version custom-go, got %v", runtimePayload["go_version"])
	}
	if _, exists := runtimePayload["goroutines"]; !exists {
		t.Fatalf("expected runtime.goroutines to be backfilled")
	}
}

func TestPokeRouteTriggersHeartbeatAndUpdatesOverview(t *testing.T) {
	mux := http.NewServeMux()
	calls := 0
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
		Poke: func(context.Context, PokeInput) error {
			calls++
			return nil
		},
	})

	pokeReq := httptest.NewRequest(http.MethodPost, "/poke", nil)
	pokeReq.Header.Set("Authorization", "Bearer token")
	pokeRec := httptest.NewRecorder()
	mux.ServeHTTP(pokeRec, pokeReq)
	if pokeRec.Code != http.StatusAccepted {
		t.Fatalf("poke status = %d, want %d (%s)", pokeRec.Code, http.StatusAccepted, pokeRec.Body.String())
	}
	if calls != 1 {
		t.Fatalf("poke calls = %d, want 1", calls)
	}

	var pokePayload map[string]any
	if err := json.Unmarshal(pokeRec.Body.Bytes(), &pokePayload); err != nil {
		t.Fatalf("poke json: %v", err)
	}
	pokedAt, _ := pokePayload["poked_at"].(string)
	if strings.TrimSpace(pokedAt) == "" {
		t.Fatalf("poked_at = %v, want non-empty string", pokePayload["poked_at"])
	}

	overviewReq := httptest.NewRequest(http.MethodGet, "/overview", nil)
	overviewReq.Header.Set("Authorization", "Bearer token")
	overviewRec := httptest.NewRecorder()
	mux.ServeHTTP(overviewRec, overviewReq)
	if overviewRec.Code != http.StatusOK {
		t.Fatalf("overview status = %d, want %d (%s)", overviewRec.Code, http.StatusOK, overviewRec.Body.String())
	}

	var overviewPayload map[string]any
	if err := json.Unmarshal(overviewRec.Body.Bytes(), &overviewPayload); err != nil {
		t.Fatalf("overview json: %v", err)
	}
	if got, _ := overviewPayload["last_poke_at"].(string); got != pokedAt {
		t.Fatalf("overview last_poke_at = %q, want %q", got, pokedAt)
	}
}

func TestPokeRouteRequiresAuthAndPost(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
		Poke: func(context.Context, PokeInput) error {
			return nil
		},
	})

	t.Run("unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/poke", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/poke", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
		if got := rec.Header().Get("Allow"); got != "POST" {
			t.Fatalf("allow = %q, want POST", got)
		}
	})
}

func TestPokeRouteReturnsConflictWhenHeartbeatAlreadyRunning(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
		Poke: func(context.Context, PokeInput) error {
			return ErrPokeBusy
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/poke", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestPokeRouteUnavailableWhenHeartbeatIsNotConfigured(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
	})

	req := httptest.NewRequest(http.MethodPost, "/poke", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestPokeRoutePassesBodyPreviewToCallback(t *testing.T) {
	mux := http.NewServeMux()
	var got PokeInput
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
		Poke: func(_ context.Context, input PokeInput) error {
			got = input
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/poke", strings.NewReader("{\"reason\":\"ci failed\"}"))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if !got.HasBody {
		t.Fatalf("expected poke input to report body presence: %#v", got)
	}
	if got.ContentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", got.ContentType)
	}
	if got.BodyText != "{\"reason\":\"ci failed\"}" {
		t.Fatalf("body text = %q, want JSON body", got.BodyText)
	}
}
