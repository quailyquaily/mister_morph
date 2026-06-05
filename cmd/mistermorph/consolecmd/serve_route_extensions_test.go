package consolecmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConsoleRouteRegistrarCanMountAuthenticatedRoute(t *testing.T) {
	previousRegistrars := consoleRouteRegistrars
	consoleRouteRegistrars = nil
	t.Cleanup(func() {
		consoleRouteRegistrars = previousRegistrars
	})

	registerConsoleRouteRegistrar(func(mux *http.ServeMux, srv *server, apiPrefix string) {
		if apiPrefix != "/console/api" {
			t.Fatalf("apiPrefix = %q, want %q", apiPrefix, "/console/api")
		}
		mux.HandleFunc(apiPrefix+"/ext/ping", srv.withAuth(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		}))
	})

	srv := &server{cfg: serveConfig{basePath: "/console", passwordOptional: true}}
	req := httptest.NewRequest(http.MethodGet, "/console/api/ext/ping", nil)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload.ok = %#v, want true", payload["ok"])
	}
}
