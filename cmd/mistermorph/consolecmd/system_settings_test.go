package consolecmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/configsettings"
	"github.com/spf13/viper"
)

func TestHandleSystemSettingsRoundTripAndReset(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initial := "# keep\nlogging:\n  level: debug\nworkspace_dir: /srv/work\nunknown: value\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", previous)
		} else {
			viper.Set("config", nil)
		}
	})

	getRec := httptest.NewRecorder()
	(&server{}).handleSystemSettings(getRec, httptest.NewRequest(http.MethodGet, "/api/settings/system", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d: %s", getRec.Code, getRec.Body.String())
	}
	var payload struct {
		ConfigRevision string         `json:"config_revision"`
		ConfigValues   map[string]any `json:"config_values"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ConfigValues["logging.level"] != "debug" || payload.ConfigValues["workspace_dir"] != "/srv/work" {
		t.Fatalf("GET values = %#v", payload.ConfigValues)
	}

	body := `{"config_revision":` + fmt.Sprintf("%q", payload.ConfigRevision) + `,"config_changes":{"logging.level":"warn","file_cache.max_files":25},"reset":["workspace_dir"]}`
	putRec := httptest.NewRecorder()
	(&server{}).handleSystemSettings(putRec, httptest.NewRequest(http.MethodPut, "/api/settings/system", strings.NewReader(body)))
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", putRec.Code, putRec.Body.String())
	}
	var putPayload struct {
		ApplyMode   configsettings.ApplyMode `json:"apply_mode"`
		ApplyStatus string                   `json:"apply_status"`
	}
	if err := json.Unmarshal(putRec.Body.Bytes(), &putPayload); err != nil {
		t.Fatal(err)
	}
	if putPayload.ApplyMode != configsettings.ApplyProcessRestart || putPayload.ApplyStatus != "pending" {
		t.Fatalf("apply result = %#v", putPayload)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if !strings.Contains(out, "# keep") || !strings.Contains(out, "unknown: value") || !strings.Contains(out, "level: warn") || !strings.Contains(out, "max_files: 25") || strings.Contains(out, "workspace_dir:") {
		t.Fatalf("updated YAML =\n%s", out)
	}
}

func TestHandleSystemSettingsRejectsStaleRevision(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("logging:\n  level: info\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", previous)
		} else {
			viper.Set("config", nil)
		}
	})

	getRec := httptest.NewRecorder()
	(&server{}).handleSystemSettings(getRec, httptest.NewRequest(http.MethodGet, "/api/settings/system", nil))
	var payload struct {
		ConfigRevision string `json:"config_revision"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	external := "logging:\n  level: error\n"
	if err := os.WriteFile(configPath, []byte(external), 0o600); err != nil {
		t.Fatal(err)
	}
	body := `{"config_revision":` + fmt.Sprintf("%q", payload.ConfigRevision) + `,"config_changes":{"logging.level":"warn"}}`
	rec := httptest.NewRecorder()
	(&server{}).handleSystemSettings(rec, httptest.NewRequest(http.MethodPut, "/api/settings/system", strings.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("PUT status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != external {
		t.Fatalf("stale update changed config:\n%s", raw)
	}
}

func TestHandleSystemSettingsRejectsInvalidWorkspace(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initial := "logging:\n  level: info\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", previous)
		} else {
			viper.Set("config", nil)
		}
	})

	missing := filepath.Join(t.TempDir(), "missing")
	body := `{"config_changes":{"workspace_dir":` + fmt.Sprintf("%q", missing) + `}}`
	rec := httptest.NewRecorder()
	(&server{}).handleSystemSettings(rec, httptest.NewRequest(http.MethodPut, "/api/settings/system", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != initial {
		t.Fatalf("invalid update changed config:\n%s", raw)
	}
}
