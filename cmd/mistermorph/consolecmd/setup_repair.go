package consolecmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/onboardingcheck"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
)

type setupRepairFilePayload struct {
	Content string `json:"content"`
}

func (s *server) handleSetupIntegrity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := onboardingcheck.BrokenItems(onboardingcheck.Check(configPath, s.setupRepairStateDir()))
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (s *server) handleSetupRepairFile(w http.ResponseWriter, r *http.Request) {
	item, err := s.resolveSetupRepairFile(r.URL.Query().Get("key"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		raw, err := os.ReadFile(item.Path)
		if err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, fmt.Sprintf("%s is missing", item.Name))
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"key":     item.Key,
			"name":    item.Name,
			"path":    item.Path,
			"content": string(raw),
		})
	case http.MethodPut:
		var req setupRepairFilePayload
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := os.MkdirAll(filepath.Dir(item.Path), 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := writeRepairFile(item.Path, []byte(req.Content)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		next, err := s.resolveSetupRepairFile(item.Key)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":   true,
			"item": next,
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) resolveSetupRepairFile(rawKey string) (onboardingcheck.Item, error) {
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		return onboardingcheck.Item{}, err
	}
	stateDir := s.setupRepairStateDir()
	switch strings.TrimSpace(rawKey) {
	case onboardingcheck.FileKeyConfig:
		return onboardingcheck.InspectConfigPath(configPath), nil
	case onboardingcheck.FileKeyIdentity:
		return onboardingcheck.InspectIdentityPath(filepath.Join(stateDir, "IDENTITY.md")), nil
	case onboardingcheck.FileKeySoul:
		return onboardingcheck.InspectSoulPath(filepath.Join(stateDir, "SOUL.md")), nil
	default:
		return onboardingcheck.Item{}, fmt.Errorf("invalid repair file key")
	}
}

func (s *server) setupRepairStateDir() string {
	if s != nil && strings.TrimSpace(s.cfg.stateDir) != "" {
		return s.cfg.stateDir
	}
	return pathutil.ResolveStateDir(viper.GetString("file_state_dir"))
}

func writeRepairFile(path string, content []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return fsstore.WriteTextAtomic(path, string(content), fsstore.FileOptions{FilePerm: mode})
}
