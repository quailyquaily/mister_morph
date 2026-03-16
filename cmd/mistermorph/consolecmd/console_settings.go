package consolecmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const consoleSettingsKey = "console"

type consoleSettingsPayload struct {
	ManagedRuntimes []string `json:"managed_runtimes"`
}

func (s *server) handleConsoleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleConsoleSettingsGet(w, r)
	case http.MethodPut:
		s.handleConsoleSettingsPut(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleConsoleSettingsGet(w http.ResponseWriter, _ *http.Request) {
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, err := readConsoleSettings(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"managed_runtimes": settings.ManagedRuntimes,
		"config_path":      configPath,
	})
}

func (s *server) handleConsoleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var req consoleSettingsPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	managedKinds, err := normalizeManagedRuntimeKinds(req.ManagedRuntimes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	serialized, err := writeConsoleSettings(configPath, consoleSettingsPayload{ManagedRuntimes: managedKinds})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tmp := viper.New()
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(serialized)); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid config yaml: %v", err))
		return
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(configPath, serialized, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	next := readConsoleSettingsFromReader(tmp)
	viper.Set("console.managed_runtimes", next.ManagedRuntimes)
	if s != nil && s.managed != nil {
		if err := s.managed.UpdateKinds(next.ManagedRuntimes); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"managed_runtimes": next.ManagedRuntimes,
		"config_path":      configPath,
	})
}

func readConsoleSettings(configPath string) (consoleSettingsPayload, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return readConsoleSettingsFromReader(viper.GetViper()), nil
		}
		return consoleSettingsPayload{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return readConsoleSettingsFromReader(viper.GetViper()), nil
	}
	tmp := viper.New()
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(data)); err != nil {
		return consoleSettingsPayload{}, fmt.Errorf("invalid config yaml: %w", err)
	}
	return readConsoleSettingsFromReader(tmp), nil
}

func writeConsoleSettings(configPath string, values consoleSettingsPayload) ([]byte, error) {
	doc, err := loadYAMLDocument(configPath)
	if err != nil {
		return nil, err
	}
	root, err := documentMapping(doc)
	if err != nil {
		return nil, err
	}
	consoleNode := ensureMappingValue(root, consoleSettingsKey)
	setMappingStringList(consoleNode, "managed_runtimes", values.ManagedRuntimes)
	return marshalYAMLDocument(doc)
}

func readConsoleSettingsFromReader(r interface {
	GetStringSlice(string) []string
}) consoleSettingsPayload {
	if r == nil {
		return consoleSettingsPayload{}
	}
	managedKinds, _ := normalizeManagedRuntimeKinds(r.GetStringSlice("console.managed_runtimes"))
	return consoleSettingsPayload{
		ManagedRuntimes: managedKinds,
	}
}
