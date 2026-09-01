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

	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/spf13/viper"
)

func TestHandleSetupIntegrityListsBrokenFiles(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() config error = %v", err)
	}
	personaDir := filepath.Join(stateDir, "persona")
	if err := os.MkdirAll(personaDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() persona error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(personaDir, "identity.yaml"), []byte("name: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() identity error = %v", err)
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

	req := httptest.NewRequest(http.MethodGet, "/api/setup/integrity", nil)
	rec := httptest.NewRecorder()

	(&server{cfg: serveConfig{stateDir: stateDir}}).handleSetupIntegrity(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Items []struct {
			Key  string `json:"key"`
			Code string `json:"code"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(payload.Items))
	}
	if payload.Items[0].Code != "config_invalid" || payload.Items[1].Code != "identity_invalid" {
		t.Fatalf("issue codes = %#v", payload.Items)
	}
}

func TestHandleSetupRepairFilePutRepairsIdentity(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "persona", "identity.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("# broken"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	body, err := json.Marshal(map[string]string{
		"content": "name: \"Momo\"\ncreature: \"cat\"\nvibe: \"calm\"\nemoji: \"cat\"\n",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPut,
		"/api/setup/file?key=identity",
		bytes.NewBuffer(body),
	)
	rec := httptest.NewRecorder()

	(&server{cfg: serveConfig{stateDir: stateDir}}).handleSetupRepairFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("expected repaired identity.yaml content")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"ok"`)) {
		t.Fatalf("response should report repaired file: %s", rec.Body.String())
	}
}

func TestHandleSetupRepairSecretPutRestoresMissingSecret(t *testing.T) {
	const id = "b_LsX7HLzAR3OShG7YjRcw"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  profiles:\n    main:\n      api_key: "+secref.OSSecretRef(id)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := viper.Get("config")
	viper.Set("config", configPath)
	t.Cleanup(func() { viper.Set("config", previous) })
	store := &consoleSettingsTestOSStore{values: map[string]string{}}
	body := `{"field_path":["llm","profiles","main","api_key"],"value":"replacement-key"}`
	req := httptest.NewRequest(http.MethodPut, "/api/setup/secret", strings.NewReader(body))
	rec := httptest.NewRecorder()

	(&server{secretStore: store}).handleSetupRepairSecret(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := store.values[id]; got != "replacement-key" {
		t.Fatalf("stored value = %q, want replacement-key", got)
	}
	if got := store.labels[id]; got != "llm.profiles.main.api_key" {
		t.Fatalf("stored label = %q, want llm.profiles.main.api_key", got)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), secref.OSSecretRef(id)) || strings.Contains(string(raw), "replacement-key") {
		t.Fatalf("config reference changed unexpectedly:\n%s", raw)
	}
}

func TestHandleSetupRepairSecretDeleteRemovesReference(t *testing.T) {
	const id = "b_LsX7HLzAR3OShG7YjRcw"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n  profiles:\n    main:\n      api_key: "+secref.OSSecretRef(id)+"\n      model: gpt-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := viper.Get("config")
	viper.Set("config", configPath)
	t.Cleanup(func() { viper.Set("config", previous) })
	store := &consoleSettingsTestOSStore{values: map[string]string{}}
	body := `{"field_path":["llm","profiles","main","api_key"]}`
	req := httptest.NewRequest(http.MethodDelete, "/api/setup/secret", strings.NewReader(body))
	rec := httptest.NewRecorder()

	(&server{secretStore: store}).handleSetupRepairSecret(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "api_key") || !strings.Contains(string(raw), "model: gpt-test") {
		t.Fatalf("config reference was not removed cleanly:\n%s", raw)
	}
}
