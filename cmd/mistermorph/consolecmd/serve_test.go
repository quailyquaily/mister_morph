package consolecmd

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestWriteJSONSetsNoCacheHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, 200, map[string]any{"ok": true})

	resp := rec.Result()
	if got := resp.Header.Get("Cache-Control"); got == "" {
		t.Fatalf("Cache-Control header is empty")
	}
	if got := resp.Header.Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want %q", got, "no-cache")
	}
	if got := resp.Header.Get("Expires"); got != "0" {
		t.Fatalf("Expires = %q, want %q", got, "0")
	}
	if got := resp.Header.Get("Vary"); got != "Authorization" {
		t.Fatalf("Vary = %q, want %q", got, "Authorization")
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	var parsed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if ok, _ := parsed["ok"].(bool); !ok {
		t.Fatalf("body.ok = %#v, want true", parsed["ok"])
	}
}

func TestNormalizeBasePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "/"},
		{"/", "/"},
		{"console", "/console"},
		{"/console/", "/console"},
	}

	for _, tc := range cases {
		got, err := normalizeBasePath(tc.in)
		if err != nil {
			t.Fatalf("normalizeBasePath(%q) error = %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeBasePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHandleSPARootBasePathDoesNotServeAPI(t *testing.T) {
	staticDir := t.TempDir()
	indexPath := filepath.Join(staticDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html>ok</html>\n"), 0o600); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	srv := &server{cfg: serveConfig{basePath: "/", staticDir: staticDir}}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	apiRec := httptest.NewRecorder()
	srv.handleSPA(apiRec, apiReq)
	if apiRec.Code != http.StatusNotFound {
		t.Fatalf("handleSPA(/api/auth/login) status = %d, want %d", apiRec.Code, http.StatusNotFound)
	}

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRec := httptest.NewRecorder()
	srv.handleSPA(rootRec, rootReq)
	if rootRec.Code != http.StatusOK {
		t.Fatalf("handleSPA(/) status = %d, want %d", rootRec.Code, http.StatusOK)
	}
}

func TestHandleSPAInjectsConfiguredBasePathIntoIndex(t *testing.T) {
	staticDir := t.TempDir()
	indexPath := filepath.Join(staticDir, "index.html")
	if err := os.WriteFile(indexPath, []byte(`<meta name="mistermorph-base-path" content="__MISTERMORPH_BASE_PATH__">`), 0o600); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	srv := &server{cfg: serveConfig{basePath: "/console", staticDir: staticDir}}
	req := httptest.NewRequest(http.MethodGet, "/console/login", nil)
	rec := httptest.NewRecorder()
	srv.handleSPA(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleSPA(/console/login) status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); !strings.Contains(body, `content="/console"`) {
		t.Fatalf("index.html missing injected base path: %s", body)
	}
}

func TestResolveStaticDir(t *testing.T) {
	staticDir := t.TempDir()
	indexPath := filepath.Join(staticDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html>ok</html>\n"), 0o600); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	cases := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "empty", in: "", want: ""},
		{name: "valid", in: staticDir, want: staticDir},
		{name: "missing index", in: t.TempDir(), wantErr: "must contain index.html"},
		{name: "missing dir", in: filepath.Join(t.TempDir(), "missing"), wantErr: "is invalid"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveStaticDir(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("resolveStaticDir(%q) error = %v, want containing %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveStaticDir(%q) error = %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("resolveStaticDir(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLoadServeConfigAllowsEmptyStaticDir(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("console.endpoints", []map[string]string{
		{
			"name":       "Main",
			"url":        "http://127.0.0.1:8787",
			"auth_token": "dev-token",
		},
	})

	cfg, err := loadServeConfig(newServeCmd())
	if err != nil {
		t.Fatalf("loadServeConfig() error = %v", err)
	}
	if cfg.staticDir != "" {
		t.Fatalf("cfg.staticDir = %q, want empty", cfg.staticDir)
	}
}

func TestLoadServeConfigSkipsIncompleteEndpoints(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("console.endpoints", []map[string]string{
		{
			"name":       "Main",
			"url":        "http://127.0.0.1:8787",
			"auth_token": "dev-token",
		},
		{
			"name": "PI",
			"url":  "http://127.0.0.1:8788",
		},
	})

	cfg, err := loadServeConfig(newServeCmd())
	if err != nil {
		t.Fatalf("loadServeConfig() error = %v", err)
	}
	if len(cfg.endpoints) != 1 {
		t.Fatalf("len(cfg.endpoints) = %d, want 1", len(cfg.endpoints))
	}
	if cfg.endpoints[0].Name != "Main" {
		t.Fatalf("cfg.endpoints[0].Name = %q, want %q", cfg.endpoints[0].Name, "Main")
	}
	if len(cfg.endpointWarnings) != 1 {
		t.Fatalf("len(cfg.endpointWarnings) = %d, want 1", len(cfg.endpointWarnings))
	}
	if !strings.Contains(cfg.endpointWarnings[0], "console.endpoints[1] skipped") {
		t.Fatalf("cfg.endpointWarnings[0] = %q, want skipped warning", cfg.endpointWarnings[0])
	}
}

func TestLoadServeConfigManagedRuntimes(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("console.managed_runtimes", []string{" telegram ", "slack", "telegram"})

	cfg, err := loadServeConfig(newServeCmd())
	if err != nil {
		t.Fatalf("loadServeConfig() error = %v", err)
	}
	if len(cfg.managedKinds) != 2 {
		t.Fatalf("len(cfg.managedKinds) = %d, want 2", len(cfg.managedKinds))
	}
	if cfg.managedKinds[0] != "telegram" || cfg.managedKinds[1] != "slack" {
		t.Fatalf("cfg.managedKinds = %#v, want [telegram slack]", cfg.managedKinds)
	}
}

func TestLoadServeConfigRejectsUnsupportedManagedRuntime(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("console.managed_runtimes", []string{"telegram", "line"})

	_, err := loadServeConfig(newServeCmd())
	if err == nil || !strings.Contains(err.Error(), "unsupported console.managed_runtimes entry") {
		t.Fatalf("loadServeConfig() error = %v, want unsupported managed runtime", err)
	}
}

func TestIsBenignServeCloseError(t *testing.T) {
	if !isBenignServeCloseError(nil) {
		t.Fatalf("nil error should be benign")
	}
	if !isBenignServeCloseError(http.ErrServerClosed) {
		t.Fatalf("http.ErrServerClosed should be benign")
	}
	if !isBenignServeCloseError(net.ErrClosed) {
		t.Fatalf("net.ErrClosed should be benign")
	}
	if isBenignServeCloseError(errors.New("boom")) {
		t.Fatalf("unexpected error should not be benign")
	}
}
