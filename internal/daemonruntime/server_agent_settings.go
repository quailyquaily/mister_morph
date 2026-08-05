package daemonruntime

import (
	"encoding/json"
	"net/http"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
)

func registerRuntimeAgentSettingsRoutes(
	mux *http.ServeMux,
	authToken string,
	owner agentsettings.Owner,
	reader agentsettings.Reader,
) {
	if owner == nil {
		owner = agentsettings.NewReadOnlyOwner(reader, "runtime settings are read-only: settings writer is unavailable")
	}
	handler := agentsettings.NewHandler(agentsettings.HandlerOptions{Owner: owner})
	register := func(path string, serve func(http.ResponseWriter, *http.Request)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !checkAuth(r, authToken) {
				writeRuntimeAuthError(w)
				return
			}
			serve(w, r)
		})
	}
	register("/settings/agent", handler.Settings)
	register("/settings/agent/models", handler.Models)
	register("/settings/agent/test", handler.Test)
}

func writeRuntimeAuthError(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Vary", "Authorization")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
}
