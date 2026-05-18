package consolecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/integration"
	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/updatecheck"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const autoUpdateSettingsKey = "auto_update"
const autoUpdateCheckTimeout = 30 * time.Second
const consoleUpdateUserAgent = "mistermorph-console"

var autoUpdateManifestURL = updatecheck.DefaultManifestURL

type autoUpdatePayload struct {
	Enabled bool `json:"enabled"`
}

type autoUpdateSettingsPayload struct {
	AutoUpdate autoUpdatePayload `json:"auto_update"`
}

type autoUpdateUpdatePayload struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type autoUpdateSettingsUpdatePayload struct {
	AutoUpdate *autoUpdateUpdatePayload `json:"auto_update,omitempty"`
}

func (s *server) handleAutoUpdateSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAutoUpdateSettingsGet(w, r)
	case http.MethodPut:
		s.handleAutoUpdateSettingsPut(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleAutoUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), autoUpdateCheckTimeout)
	defer cancel()

	result, err := updatecheck.Check(ctx, updatecheck.Options{
		CurrentVersion: s.autoUpdateCurrentVersion(),
		ManifestURL:    autoUpdateManifestURL,
		UserAgent:      consoleUpdateUserAgent,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) autoUpdateCurrentVersion() string {
	if s != nil {
		if v := strings.TrimSpace(s.cfg.version); v != "" {
			return v
		}
	}
	return "dev"
}

func (s *server) handleAutoUpdateSettingsGet(w http.ResponseWriter, _ *http.Request) {
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, err := readAutoUpdateSettings(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"auto_update": settings.AutoUpdate,
		"config_path": configPath,
	})
}

func (s *server) handleAutoUpdateSettingsPut(w http.ResponseWriter, r *http.Request) {
	var req autoUpdateSettingsUpdatePayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	current, err := readAutoUpdateSettings(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next := normalizeAutoUpdateSettingsUpdatePayload(current, req)
	serialized, err := writeAutoUpdateSettings(configPath, next)
	if err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"auto_update": next.AutoUpdate,
		"config_path": configPath,
	})
}

func readAutoUpdateSettings(configPath string) (autoUpdateSettingsPayload, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultAutoUpdateSettingsPayload(), nil
		}
		return autoUpdateSettingsPayload{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return defaultAutoUpdateSettingsPayload(), nil
	}
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(data)); err != nil {
		return autoUpdateSettingsPayload{}, fmt.Errorf("invalid config yaml: %w", err)
	}
	return readAutoUpdateSettingsFromReader(tmp), nil
}

func defaultAutoUpdateSettingsPayload() autoUpdateSettingsPayload {
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	return readAutoUpdateSettingsFromReader(tmp)
}

func readAutoUpdateSettingsFromReader(r interface {
	GetBool(string) bool
}) autoUpdateSettingsPayload {
	if r == nil {
		return autoUpdateSettingsPayload{}
	}
	return autoUpdateSettingsPayload{
		AutoUpdate: autoUpdatePayload{
			Enabled: r.GetBool("auto_update.enabled"),
		},
	}
}

func normalizeAutoUpdateSettingsUpdatePayload(
	current autoUpdateSettingsPayload,
	in autoUpdateSettingsUpdatePayload,
) autoUpdateSettingsPayload {
	next := current
	if in.AutoUpdate != nil && in.AutoUpdate.Enabled != nil {
		next.AutoUpdate.Enabled = *in.AutoUpdate.Enabled
	}
	return next
}

func writeAutoUpdateSettings(configPath string, values autoUpdateSettingsPayload) ([]byte, error) {
	doc, err := loadYAMLDocument(configPath)
	if err != nil {
		return nil, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	removeLegacyDesktopAutoUpdate(root)
	autoUpdateNode := configbootstrap.EnsureMappingValue(root, autoUpdateSettingsKey)
	configbootstrap.SetMappingBoolValue(autoUpdateNode, "enabled", values.AutoUpdate.Enabled)
	return configbootstrap.MarshalDocument(doc)
}

func removeLegacyDesktopAutoUpdate(root *yaml.Node) {
	desktopNode := configbootstrap.FindMappingValue(root, "desktop")
	if desktopNode == nil || desktopNode.Kind != yaml.MappingNode {
		return
	}
	configbootstrap.DeleteMappingKey(desktopNode, "auto_update")
	if len(desktopNode.Content) == 0 {
		configbootstrap.DeleteMappingKey(root, "desktop")
	}
}
