package consolecmd

import (
	"net/http"

	"github.com/quailyquaily/mistermorph/internal/runtimecommands"
)

func (s *server) handleRuntimeCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": runtimecommands.Suggestions(),
	})
}
