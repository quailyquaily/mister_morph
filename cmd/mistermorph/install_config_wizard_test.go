package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/platformutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func loadPatchedConfig(t *testing.T, body string) *viper.Viper {
	t.Helper()
	tmp := viper.New()
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(strings.NewReader(body)); err != nil {
		t.Fatalf("ReadConfig() error = %v\nconfig:\n%s", err, body)
	}
	return tmp
}

func TestFindReadableInstallConfigPriority(t *testing.T) {
	initViperDefaults()

	root := t.TempDir()
	installDir := filepath.Join(root, "install")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("mkdir install dir: %v", err)
	}

	flagCfgPath := filepath.Join(root, "cfg-from-flag.yaml")
	if err := os.WriteFile(flagCfgPath, []byte("llm:\n  provider: openai\n"), 0o644); err != nil {
		t.Fatalf("write flag config: %v", err)
	}

	dirCfgPath := filepath.Join(installDir, "config.yaml")
	if err := os.WriteFile(dirCfgPath, []byte("llm:\n  provider: gemini\n"), 0o644); err != nil {
		t.Fatalf("write dir config: %v", err)
	}

	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	morphHome := filepath.Join(home, ".morph")
	if err := os.MkdirAll(morphHome, 0o755); err != nil {
		t.Fatalf("mkdir ~/.morph: %v", err)
	}
	homeCfgPath := filepath.Join(morphHome, "config.yaml")
	if err := os.WriteFile(homeCfgPath, []byte("llm:\n  provider: cloudflare\n"), 0o644); err != nil {
		t.Fatalf("write ~/.morph/config.yaml: %v", err)
	}

	prevConfig := viper.GetString("config")
	viper.Set("config", flagCfgPath)
	t.Cleanup(func() {
		if prevConfig == "" {
			viper.Set("config", nil)
			return
		}
		viper.Set("config", prevConfig)
	})

	if got, ok := findReadableInstallConfig(nil, installDir); !ok || got != flagCfgPath {
		t.Fatalf("findReadableInstallConfig() = (%q, %v), want (%q, true)", got, ok, flagCfgPath)
	}

	viper.Set("config", "")
	if got, ok := findReadableInstallConfig(nil, installDir); !ok || got != dirCfgPath {
		t.Fatalf("findReadableInstallConfig() = (%q, %v), want (%q, true)", got, ok, dirCfgPath)
	}

	if err := os.Remove(dirCfgPath); err != nil {
		t.Fatalf("remove dir config: %v", err)
	}
	if got, ok := findReadableInstallConfig(nil, installDir); !ok || got != homeCfgPath {
		t.Fatalf("findReadableInstallConfig() = (%q, %v), want (%q, true)", got, ok, homeCfgPath)
	}
}

func TestMaybeCollectInstallConfigSetup_NonInteractiveSkipsWizard(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBufferString(""))
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	setup, err := maybeCollectInstallConfigSetup(cmd, false)
	if err != nil {
		t.Fatalf("maybeCollectInstallConfigSetup() error = %v", err)
	}
	if setup != nil {
		t.Fatalf("expected nil setup in non-interactive mode")
	}
	if !strings.Contains(errOut.String(), "non-interactive mode detected") {
		t.Fatalf("expected warning about non-interactive mode, got: %q", errOut.String())
	}
}

func TestPatchInitConfigWithSetup_AppliesOverrides(t *testing.T) {
	body, err := loadConfigExample()
	if err != nil {
		t.Fatalf("loadConfigExample() error = %v", err)
	}

	setup := &installConfigSetup{
		Provider:           "cloudflare",
		Endpoint:           "https://api.cloudflare.com/client/v4",
		Model:              "@cf/meta/llama-3.1-8b-instruct",
		CloudflareAccount:  "acc-123",
		CloudflareAPIToken: "token-xyz",
	}

	got, err := patchInitConfigWithSetup(body, "/tmp/my-state", setup)
	if err != nil {
		t.Fatalf("patchInitConfigWithSetup() error = %v", err)
	}

	cfg := loadPatchedConfig(t, got)

	if gotPath := cfg.GetString("file_state_dir"); gotPath != "/tmp/my-state" {
		t.Fatalf("file_state_dir = %q, want /tmp/my-state", gotPath)
	}
	if gotProvider := cfg.GetString("llm.provider"); gotProvider != "cloudflare" {
		t.Fatalf("llm.provider = %q, want cloudflare", gotProvider)
	}
	if gotEndpoint := cfg.GetString("llm.endpoint"); gotEndpoint != "https://api.cloudflare.com/client/v4" {
		t.Fatalf("llm.endpoint = %q, want cloudflare endpoint", gotEndpoint)
	}
	if gotModel := cfg.GetString("llm.model"); gotModel != "@cf/meta/llama-3.1-8b-instruct" {
		t.Fatalf("llm.model = %q, want cloudflare model", gotModel)
	}
	if gotAccountID := cfg.GetString("llm.cloudflare.account_id"); gotAccountID != "acc-123" {
		t.Fatalf("llm.cloudflare.account_id = %q, want acc-123", gotAccountID)
	}
	if gotToken := cfg.GetString("llm.cloudflare.api_token"); gotToken != "token-xyz" {
		t.Fatalf("llm.cloudflare.api_token = %q, want token-xyz", gotToken)
	}
	if gotAPIKey := cfg.GetString("llm.api_key"); gotAPIKey != "" {
		t.Fatalf("llm.api_key = %q, want empty for cloudflare", gotAPIKey)
	}
	if gotSources := cfg.GetStringSlice("multimodal.image.sources"); len(gotSources) != 0 {
		t.Fatalf("multimodal.image.sources = %#v, want empty", gotSources)
	}
	var endpoints []map[string]any
	if err := cfg.UnmarshalKey("console.endpoints", &endpoints); err != nil {
		t.Fatalf("UnmarshalKey(console.endpoints) error = %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("console.endpoints = %#v, want empty", endpoints)
	}
	if strings.Contains(got, "tg-token") || strings.Contains(got, "xoxb-test") || strings.Contains(got, "console-secret") {
		t.Fatalf("patched config should not include removed onboarding integrations: %s", got)
	}
}

func TestPatchInitConfigWithSetup_OpenAICompatiblePrunesCloudflareBlock(t *testing.T) {
	body, err := loadConfigExample()
	if err != nil {
		t.Fatalf("loadConfigExample() error = %v", err)
	}

	got, err := patchInitConfigWithSetup(body, "/tmp/my-state", &installConfigSetup{
		Provider: setupProviderOpenAICompatible,
		Endpoint: "https://api.deepseek.com",
		Model:    "deepseek-chat",
		APIKey:   "sk-openai-compatible",
	})
	if err != nil {
		t.Fatalf("patchInitConfigWithSetup() error = %v", err)
	}
	cfg := loadPatchedConfig(t, got)

	if gotProvider := cfg.GetString("llm.provider"); gotProvider != "openai" {
		t.Fatalf("llm.provider = %q, want openai", gotProvider)
	}
	if strings.Contains(got, "provider: openai_custom") {
		t.Fatalf("patched config should not write openai_custom: %s", got)
	}
	if gotEndpoint := cfg.GetString("llm.endpoint"); gotEndpoint != "https://api.deepseek.com" {
		t.Fatalf("llm.endpoint = %q, want https://api.deepseek.com", gotEndpoint)
	}
	if gotModel := cfg.GetString("llm.model"); gotModel != "deepseek-chat" {
		t.Fatalf("llm.model = %q, want deepseek-chat", gotModel)
	}
	if gotAPIKey := cfg.GetString("llm.api_key"); gotAPIKey != "sk-openai-compatible" {
		t.Fatalf("llm.api_key = %q, want sk-openai-compatible", gotAPIKey)
	}
	if strings.Contains(got, "\n  cloudflare:\n") || strings.Contains(got, "account_id:") || strings.Contains(got, "api_token:") {
		t.Fatalf("patched config should not include cloudflare block: %s", got)
	}
}

func TestPatchInitConfigWithSetup_DefaultPrunesCloudflareBlock(t *testing.T) {
	body, err := loadConfigExample()
	if err != nil {
		t.Fatalf("loadConfigExample() error = %v", err)
	}

	got, err := patchInitConfigWithSetup(body, "/tmp/my-state", nil)
	if err != nil {
		t.Fatalf("patchInitConfigWithSetup() error = %v", err)
	}
	cfg := loadPatchedConfig(t, got)

	if strings.Contains(got, "\n  cloudflare:\n") || strings.Contains(got, "account_id:") || strings.Contains(got, "api_token:") {
		t.Fatalf("default patched config should not include cloudflare block: %s", got)
	}
	if strings.Contains(got, "\n  endpoint: \"https://api.openai.com\"") ||
		strings.Contains(got, "\n  model: \"gpt-5.4\"") ||
		strings.Contains(got, "\n  api_key: \"${OPENAI_API_KEY}\"") {
		t.Fatalf("default patched config should clear template llm examples: %s", got)
	}
	if gotProvider := cfg.GetString("llm.provider"); gotProvider != "openai" {
		t.Fatalf("llm.provider = %q, want openai", gotProvider)
	}
	var endpoints []map[string]any
	if err := cfg.UnmarshalKey("console.endpoints", &endpoints); err != nil {
		t.Fatalf("UnmarshalKey(console.endpoints) error = %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("console.endpoints = %#v, want empty", endpoints)
	}
	expectedBash := true
	expectedPowerShell := false
	if platformutil.IsWindows() {
		expectedBash = false
		expectedPowerShell = true
	}
	if gotBash := cfg.GetBool("tools.bash.enabled"); gotBash != expectedBash {
		t.Fatalf("tools.bash.enabled = %v, want %v", gotBash, expectedBash)
	}
	if gotPowerShell := cfg.GetBool("tools.powershell.enabled"); gotPowerShell != expectedPowerShell {
		t.Fatalf("tools.powershell.enabled = %v, want %v", gotPowerShell, expectedPowerShell)
	}
}

func TestNormalizeConsoleBasePath(t *testing.T) {
	cases := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "", want: "/", ok: true},
		{input: "console", want: "/console", ok: true},
		{input: "/console/", want: "/console", ok: true},
		{input: "/a/b/", want: "/a/b", ok: true},
		{input: "/", want: "/", ok: true},
	}
	for _, tc := range cases {
		got, err := normalizeConsoleBasePath(tc.input)
		if tc.ok && err != nil {
			t.Fatalf("normalizeConsoleBasePath(%q) error: %v", tc.input, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("normalizeConsoleBasePath(%q) expected error", tc.input)
		}
		if tc.ok && got != tc.want {
			t.Fatalf("normalizeConsoleBasePath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestProbeConsoleEndpointHealth(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q, want /health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(okServer.Close)

	ok, detail := probeConsoleEndpointHealth(okServer.URL)
	if !ok {
		t.Fatalf("expected health check success, got failed: %s", detail)
	}

	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	t.Cleanup(failServer.Close)

	ok, detail = probeConsoleEndpointHealth(failServer.URL)
	if ok {
		t.Fatalf("expected health check failure")
	}
	if !strings.Contains(detail, "503") {
		t.Fatalf("failure detail should include status, got: %q", detail)
	}
}

func TestIsLikelyLocalEndpointURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{url: "http://127.0.0.1:8787", want: true},
		{url: "http://localhost:8787", want: true},
		{url: "http://[::1]:8787", want: true},
		{url: "https://example.com", want: false},
	}
	for _, tc := range cases {
		got := isLikelyLocalEndpointURL(tc.url)
		if got != tc.want {
			t.Fatalf("isLikelyLocalEndpointURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestSetupSuggestedEnvVarLinesIncludesGeneratedLocalToken(t *testing.T) {
	setup := &installConfigSetup{
		ConsoleEndpointAuthTokenEnv: "MISTER_MORPH_SERVER_AUTH_TOKEN",
		ServerAuthTokenEnv:          "MISTER_MORPH_SERVER_AUTH_TOKEN",
		GeneratedServerAuthToken:    "abc123",
	}
	lines := setupSuggestedEnvVarLines(setup)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, `MISTER_MORPH_SERVER_AUTH_TOKEN`) {
		t.Fatalf("expected auth token env var in suggestions, got: %v", lines)
	}
	if !strings.Contains(joined, `export MISTER_MORPH_SERVER_AUTH_TOKEN="abc123"`) {
		t.Fatalf("expected export command for generated local token, got: %v", lines)
	}
}
