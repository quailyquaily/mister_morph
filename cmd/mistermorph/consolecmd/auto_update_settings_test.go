package consolecmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestReadAutoUpdateSettings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"auto_update:\n  enabled: true\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := readAutoUpdateSettings(configPath)
	if err != nil {
		t.Fatalf("readAutoUpdateSettings() error = %v", err)
	}
	if !got.AutoUpdate.Enabled {
		t.Fatalf("auto update enabled = false, want true")
	}
}

func TestReadAutoUpdateSettingsDefaultDisabled(t *testing.T) {
	got, err := readAutoUpdateSettings(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("readAutoUpdateSettings() error = %v", err)
	}
	if got.AutoUpdate.Enabled {
		t.Fatalf("auto update enabled = true, want false")
	}
}

func TestWriteAutoUpdateSettingsPreservesOtherConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n  provider: openai\n  model: gpt-5.2\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	serialized, err := writeAutoUpdateSettings(configPath, autoUpdateSettingsPayload{
		AutoUpdate: autoUpdatePayload{Enabled: true},
	})
	if err != nil {
		t.Fatalf("writeAutoUpdateSettings() error = %v", err)
	}
	out := string(serialized)
	if !strings.Contains(out, "provider: openai") || !strings.Contains(out, "model: gpt-5.2") {
		t.Fatalf("serialized config lost existing settings: %s", out)
	}
	if strings.Contains(out, "desktop:") || !strings.Contains(out, "auto_update:") || !strings.Contains(out, "enabled: true") {
		t.Fatalf("serialized config has wrong auto update settings: %s", out)
	}
}

func TestWriteAutoUpdateSettingsRemovesLegacyDesktopAutoUpdate(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"desktop:\n  auto_update:\n    enabled: false\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	serialized, err := writeAutoUpdateSettings(configPath, autoUpdateSettingsPayload{
		AutoUpdate: autoUpdatePayload{Enabled: true},
	})
	if err != nil {
		t.Fatalf("writeAutoUpdateSettings() error = %v", err)
	}
	out := string(serialized)
	if strings.Contains(out, "desktop:") || !strings.Contains(out, "auto_update:") || !strings.Contains(out, "enabled: true") {
		t.Fatalf("serialized config did not migrate auto update settings: %s", out)
	}
}

func TestHandleAutoUpdateSettingsPut(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"auto_update:\n  enabled: false\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/auto-update", bytes.NewBufferString(`{
		"auto_update":{"enabled":true}
	}`))
	rec := httptest.NewRecorder()

	(&server{}).handleAutoUpdateSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	got, err := readAutoUpdateSettings(configPath)
	if err != nil {
		t.Fatalf("readAutoUpdateSettings() error = %v", err)
	}
	if !got.AutoUpdate.Enabled {
		t.Fatalf("auto update enabled = false, want true")
	}
	var payload struct {
		OK         bool              `json:"ok"`
		AutoUpdate autoUpdatePayload `json:"auto_update"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.OK || !payload.AutoUpdate.Enabled {
		t.Fatalf("payload = %#v, want ok auto_update.enabled", payload)
	}
}
