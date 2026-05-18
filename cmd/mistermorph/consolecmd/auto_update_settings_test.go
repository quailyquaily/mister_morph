package consolecmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/updatecheck"
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

func TestHandleAutoUpdateSettingsGetIncludesCurrentVersion(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/api/settings/auto-update", nil)
	rec := httptest.NewRecorder()

	(&server{cfg: serveConfig{version: "0.2.41"}}).handleAutoUpdateSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		CurrentVersion string `json:"current_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.CurrentVersion != "0.2.41" {
		t.Fatalf("current_version = %q, want 0.2.41", payload.CurrentVersion)
	}
}

func TestHandleAutoUpdateCheck(t *testing.T) {
	asset := []byte("desktop update asset")
	var manifestServer *httptest.Server
	manifestServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/update.json":
			sum := sha256.Sum256(asset)
			_ = json.NewEncoder(w).Encode(updatecheck.Manifest{
				Version:     "0.2.42",
				ReleaseDate: "2026-03-29T12:34:56Z",
				Platforms: map[string]updatecheck.Platform{
					updatecheck.PlatformKey(runtime.GOOS, runtime.GOARCH): {
						URL:      manifestServer.URL + "/asset.tar.gz",
						Size:     int64(len(asset)),
						Checksum: "sha256:" + hex.EncodeToString(sum[:]),
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(manifestServer.Close)

	previousManifestURL := autoUpdateManifestURL
	autoUpdateManifestURL = manifestServer.URL + "/update.json"
	t.Cleanup(func() {
		autoUpdateManifestURL = previousManifestURL
	})

	req := httptest.NewRequest(http.MethodPost, "/api/settings/auto-update/check", nil)
	rec := httptest.NewRecorder()

	(&server{cfg: serveConfig{version: "0.2.41"}}).handleAutoUpdateCheck(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload updatecheck.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Status != "update_available" || !payload.UpdateAvailable {
		t.Fatalf("payload = %#v, want update_available", payload)
	}
	if payload.CurrentVersion != "0.2.41" || payload.LatestVersion != "0.2.42" {
		t.Fatalf("versions = %q -> %q, want 0.2.41 -> 0.2.42", payload.CurrentVersion, payload.LatestVersion)
	}
	if payload.Downloaded {
		t.Fatalf("Downloaded = true, want false")
	}
}
