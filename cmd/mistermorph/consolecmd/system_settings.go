package consolecmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configrevision"
	"github.com/quailyquaily/mistermorph/internal/configsettings"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/spf13/viper"
)

type systemSettingsUpdate struct {
	ConfigRevision string                     `json:"config_revision"`
	ConfigChanges  map[string]json.RawMessage `json:"config_changes,omitempty"`
	Reset          []string                   `json:"reset,omitempty"`
}

func (s *server) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleSystemSettingsGet(w)
	case http.MethodPut:
		s.settingsWriteMu.Lock()
		defer s.settingsWriteMu.Unlock()
		s.handleSystemSettingsPut(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleSystemSettingsGet(w http.ResponseWriter) {
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	view, err := configsettings.ReadFile(configPath, configsettings.SystemFields())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	configsettings.ApplyRuntimeOverrides(&view, s.cfg.runtimeOverrides)
	writeJSON(w, http.StatusOK, map[string]any{
		"config_path":     configPath,
		"config_revision": view.ConfigRevision,
		"config_values":   view.Values,
		"field_states":    view.FieldStates,
	})
}

func (s *server) handleSystemSettingsPut(w http.ResponseWriter, r *http.Request) {
	var request systemSettingsUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(request.ConfigChanges) == 0 && len(request.Reset) == 0 {
		writeError(w, http.StatusBadRequest, "no settings changes")
		return
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	snapshot, err := configrevision.Read(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if expected := strings.TrimSpace(request.ConfigRevision); expected != "" && expected != snapshot.Revision {
		writeError(w, http.StatusConflict, "config changed; reload settings and try again")
		return
	}
	update := configsettings.Update{Changes: request.ConfigChanges, Reset: request.Reset}
	if err := configsettings.RejectRuntimeOverrideUpdate(update, s.cfg.runtimeOverrides); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	serialized, err := configsettings.Apply(snapshot.Data, update, configsettings.SystemFields())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateSystemSettings(serialized); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := fsstore.WriteTextAtomic(configPath, string(serialized), fsstore.FileOptions{DirPerm: 0o755, FilePerm: 0o600}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	view, err := configsettings.View(serialized, configsettings.SystemFields())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	configsettings.ApplyRuntimeOverrides(&view, s.cfg.runtimeOverrides)
	applyResult := configsettings.ResultForUpdate(update, configsettings.SystemFields(), []string{"process"})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"config_path":     configPath,
		"config_revision": configrevision.Hash(serialized),
		"config_values":   view.Values,
		"field_states":    view.FieldStates,
		"apply_mode":      applyResult.ApplyMode,
		"apply_status":    applyResult.ApplyStatus,
		"restart_targets": applyResult.RestartTargets,
	})
}

func validateSystemSettings(raw []byte) error {
	reader := viper.New()
	configdefaults.Apply(reader)
	reader.SetConfigType("yaml")
	if err := reader.ReadConfig(bytes.NewReader(raw)); err != nil {
		return err
	}
	workspaceDir := strings.TrimSpace(reader.GetString("workspace_dir"))
	if strings.Contains(workspaceDir, "${") {
		return nil
	}
	_, err := workspace.ValidateDefaultDir(workspaceDir)
	if err != nil {
		return fmt.Errorf("workspace_dir: %w", err)
	}
	return nil
}
