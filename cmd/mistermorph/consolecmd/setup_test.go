package consolecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
)

func TestLoadServeConfig_SetupModeAllowsIncompleteConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	staticDir := makeStaticDirForTest(t)
	viper.Set("console.static_dir", staticDir)
	viper.Set("console.password", "")
	viper.Set("console.password_hash", "")
	viper.Set("console.endpoints", []map[string]any{})
	viper.Set("llm.provider", "openai")
	viper.Set("llm.model", "gpt-5.2")
	viper.Set("llm.api_key", "sk-live")

	cmd := newServeCmd()
	if _, err := loadServeConfig(cmd); err == nil {
		t.Fatalf("expected strict mode to reject missing endpoints")
	}

	if err := cmd.Flags().Set("console-setup-mode", "true"); err != nil {
		t.Fatalf("set setup mode flag: %v", err)
	}
	cfg, err := loadServeConfig(cmd)
	if err != nil {
		t.Fatalf("loadServeConfig in setup mode: %v", err)
	}
	if !cfg.setupMode {
		t.Fatalf("setup mode should be enabled")
	}
	srv, err := newServer(cfg)
	if err != nil {
		t.Fatalf("newServer in setup mode: %v", err)
	}
	if !srv.isSetupRequired() {
		t.Fatalf("expected setup_required mode")
	}
	if !containsString(srv.setupStatus.MissingFields, "console.password_hash") {
		t.Fatalf("missing fields should include console.password_hash: %#v", srv.setupStatus.MissingFields)
	}
	if !containsString(srv.setupStatus.MissingFields, "console.endpoints") {
		t.Fatalf("missing fields should include console.endpoints: %#v", srv.setupStatus.MissingFields)
	}
}

func TestSetupModeGatesBusinessHandlers(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	staticDir := makeStaticDirForTest(t)
	viper.Set("console.static_dir", staticDir)
	viper.Set("console.password", "")
	viper.Set("console.password_hash", "")
	viper.Set("console.endpoints", []map[string]any{})
	viper.Set("llm.provider", "openai")
	viper.Set("llm.model", "gpt-5.2")
	viper.Set("llm.api_key", "sk-live")

	cmd := newServeCmd()
	if err := cmd.Flags().Set("console-setup-mode", "true"); err != nil {
		t.Fatalf("set setup mode flag: %v", err)
	}
	cfg, err := loadServeConfig(cmd)
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	srv, err := newServer(cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/console/api/setup/status", nil)
	srv.handleSetupStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status code = %d, want 200", rec.Code)
	}

	var setupResp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&setupResp); err != nil {
		t.Fatalf("decode setup response: %v", err)
	}
	if got := strings.TrimSpace(asString(setupResp["mode"])); got != setupModeRequired {
		t.Fatalf("setup mode = %q, want %q", got, setupModeRequired)
	}

	gated := srv.withSetupGate(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/console/api/endpoints", nil)
	gated(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("gated handler code = %d, want 409", rec.Code)
	}
	var gateResp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&gateResp); err != nil {
		t.Fatalf("decode gated response: %v", err)
	}
	if got := strings.TrimSpace(asString(gateResp["code"])); got != setupModeRequired {
		t.Fatalf("gated response code = %q, want %q", got, setupModeRequired)
	}
}

func TestSetupApplyPersistsConfigAndRequiresRestart(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	staticDir := makeStaticDirForTest(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	viper.Set("config", configPath)
	viper.Set("console.static_dir", staticDir)
	viper.Set("console.password", "")
	viper.Set("console.password_hash", "")
	viper.Set("console.endpoints", []map[string]any{})
	viper.Set("llm.provider", "openai")
	viper.Set("llm.model", "gpt-5.2")
	viper.Set("llm.api_key", "")

	cmd := newServeCmd()
	if err := cmd.Flags().Set("console-setup-mode", "true"); err != nil {
		t.Fatalf("set setup mode flag: %v", err)
	}
	cfg, err := loadServeConfig(cmd)
	if err != nil {
		t.Fatalf("loadServeConfig: %v", err)
	}
	srv, err := newServer(cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	validated := false
	srv.llmValidator = func(ctx context.Context, route llmutil.ResolvedRoute) error {
		validated = true
		if got := strings.TrimSpace(route.ClientConfig.APIKey); got != "sk-test" {
			t.Fatalf("validator api key = %q, want %q", got, "sk-test")
		}
		return nil
	}

	payload := map[string]any{
		"llm": map[string]any{
			"provider": "openai",
			"model":    "gpt-5.2",
			"endpoint": "https://api.openai.com",
			"api_key":  "sk-test",
		},
		"console": map[string]any{
			"password": "super-secret",
			"endpoints": []map[string]any{
				{
					"name":       "Primary",
					"url":        "http://127.0.0.1:8787",
					"auth_token": "daemon-token",
				},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/console/api/setup/apply", bytes.NewReader(raw))
	srv.handleSetupApply(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup apply code = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !validated {
		t.Fatalf("expected llm validator to be called")
	}

	var applyResp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&applyResp); err != nil {
		t.Fatalf("decode apply response: %v", err)
	}
	if ok, _ := applyResp["ok"].(bool); !ok {
		t.Fatalf("apply response ok = %#v, want true", applyResp["ok"])
	}
	if restartRequired, _ := applyResp["restart_required"].(bool); !restartRequired {
		t.Fatalf("restart_required = %#v, want true", applyResp["restart_required"])
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	var cfgDoc map[string]any
	if err := yaml.Unmarshal(data, &cfgDoc); err != nil {
		t.Fatalf("decode yaml: %v", err)
	}

	llmNode := asMap(cfgDoc["llm"])
	consoleNode := asMap(cfgDoc["console"])
	if got := strings.TrimSpace(asString(llmNode["provider"])); got != "openai" {
		t.Fatalf("llm.provider = %q, want openai", got)
	}
	if got := strings.TrimSpace(asString(llmNode["api_key"])); got != "sk-test" {
		t.Fatalf("llm.api_key = %q, want sk-test", got)
	}
	if got := strings.TrimSpace(asString(consoleNode["password"])); got != "" {
		t.Fatalf("console.password should be empty, got %q", got)
	}
	hash := strings.TrimSpace(asString(consoleNode["password_hash"]))
	if hash == "" || hash == "super-secret" {
		t.Fatalf("console.password_hash should be bcrypt hash, got %q", hash)
	}

	endpoints, _ := consoleNode["endpoints"].([]any)
	if len(endpoints) != 1 {
		t.Fatalf("console.endpoints len = %d, want 1", len(endpoints))
	}
	first := asMap(endpoints[0])
	if got := strings.TrimSpace(asString(first["auth_token"])); got != "daemon-token" {
		t.Fatalf("console.endpoints[0].auth_token = %q, want daemon-token", got)
	}
}

func makeStaticDirForTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>\n"), 0o600); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	return dir
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(want) {
			return true
		}
	}
	return false
}

func asString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		return ""
	}
}

func asMap(v any) map[string]any {
	out, _ := v.(map[string]any)
	if out == nil {
		return map[string]any{}
	}
	return out
}
