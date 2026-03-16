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

func TestReadConsoleSettings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"console:\n  managed_runtimes: [telegram, slack]\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := readConsoleSettings(configPath)
	if err != nil {
		t.Fatalf("readConsoleSettings() error = %v", err)
	}
	if len(got.ManagedRuntimes) != 2 || got.ManagedRuntimes[0] != "telegram" || got.ManagedRuntimes[1] != "slack" {
		t.Fatalf("got.ManagedRuntimes = %#v, want [telegram slack]", got.ManagedRuntimes)
	}
}

func TestWriteConsoleSettingsPreservesOtherConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"console:\n  listen: 127.0.0.1:9080\n"+
			"llm:\n  provider: openai\n  model: gpt-5.2\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	serialized, err := writeConsoleSettings(configPath, consoleSettingsPayload{
		ManagedRuntimes: []string{"telegram"},
	})
	if err != nil {
		t.Fatalf("writeConsoleSettings() error = %v", err)
	}
	out := string(serialized)
	if !strings.Contains(out, "listen: 127.0.0.1:9080") || !strings.Contains(out, "provider: openai") {
		t.Fatalf("serialized config lost existing settings: %s", out)
	}
	if !strings.Contains(out, "managed_runtimes:") || !strings.Contains(out, "- telegram") {
		t.Fatalf("serialized config missing managed runtimes: %s", out)
	}
}

func TestHandleConsoleSettingsPut(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"console:\n  managed_runtimes: [telegram]\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevConsole, hadConsole := viper.Get("console"), viper.IsSet("console")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadConsole {
			viper.Set("console", prevConsole)
		} else {
			viper.Set("console", nil)
		}
	})

	body := bytes.NewBufferString(`{"managed_runtimes":["slack","telegram","slack"]}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/console", body)
	rec := httptest.NewRecorder()

	(&server{managed: newManagedRuntimeSupervisor(nil, nil)}).handleConsoleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(raw), "- slack") || !strings.Contains(string(raw), "- telegram") {
		t.Fatalf("config missing managed runtime update: %s", string(raw))
	}
	got := viper.GetStringSlice("console.managed_runtimes")
	if len(got) != 2 || got[0] != "slack" || got[1] != "telegram" {
		t.Fatalf("viper managed runtimes = %#v, want [slack telegram]", got)
	}

	var payload struct {
		OK              bool     `json:"ok"`
		ManagedRuntimes []string `json:"managed_runtimes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.OK {
		t.Fatalf("payload.OK = false, want true")
	}
	if len(payload.ManagedRuntimes) != 2 || payload.ManagedRuntimes[0] != "slack" || payload.ManagedRuntimes[1] != "telegram" {
		t.Fatalf("payload.ManagedRuntimes = %#v, want [slack telegram]", payload.ManagedRuntimes)
	}
}

func TestHandleConsoleSettingsPutRejectsInvalidRuntime(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/settings/console", bytes.NewBufferString(`{"managed_runtimes":["line"]}`))
	rec := httptest.NewRecorder()

	(&server{}).handleConsoleSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
