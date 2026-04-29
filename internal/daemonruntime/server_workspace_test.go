package daemonruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestWorkspaceRouteGet(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceGet: func(_ context.Context, topicID string) (string, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return "/repo/project", nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/workspace?topic_id=topic_a", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["topic_id"] != "topic_a" {
		t.Fatalf("payload.topic_id = %#v, want %q", payload["topic_id"], "topic_a")
	}
	if payload["workspace_dir"] != "/repo/project" {
		t.Fatalf("payload.workspace_dir = %#v, want %q", payload["workspace_dir"], "/repo/project")
	}
}

func TestWorkspaceRoutePut(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspacePut: func(_ context.Context, topicID string, workspaceDir string) (string, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			if workspaceDir != "./repo" {
				t.Fatalf("workspaceDir = %q, want %q", workspaceDir, "./repo")
			}
			return "/repo/project", nil
		},
	})

	req := httptest.NewRequest(http.MethodPut, "/workspace", strings.NewReader(`{"topic_id":"topic_a","workspace_dir":"./repo"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["workspace_dir"] != "/repo/project" {
		t.Fatalf("payload.workspace_dir = %#v, want %q", payload["workspace_dir"], "/repo/project")
	}
}

func TestWorkspaceRouteDelete(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceDelete: func(_ context.Context, topicID string) error {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/workspace?topic_id=topic_a", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["workspace_dir"] != "" {
		t.Fatalf("payload.workspace_dir = %#v, want empty", payload["workspace_dir"])
	}
}

func TestWorkspaceTreeRouteGet(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceTree: func(_ context.Context, topicID string, treePath string) (WorkspaceTreeListing, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			if treePath != "src" {
				t.Fatalf("treePath = %q, want %q", treePath, "src")
			}
			return WorkspaceTreeListing{
				RootPath: "/repo/project",
				Path:     "src",
				Items: []WorkspaceTreeEntry{
					{Name: "main.go", Path: "src/main.go", IsDir: false, SizeBytes: 42},
				},
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/workspace/tree?topic_id=topic_a&path=src", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["path"] != "src" {
		t.Fatalf("payload.path = %#v, want %q", payload["path"], "src")
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("payload.items = %#v, want one item", payload["items"])
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("payload.items[0] = %#v, want object", items[0])
	}
	if first["size_bytes"] != float64(42) {
		t.Fatalf("payload.items[0].size_bytes = %#v, want %v", first["size_bytes"], float64(42))
	}
}

func TestWorkspaceBrowseRouteGet(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceBrowse: func(_ context.Context, treePath string) (WorkspaceTreeListing, error) {
			if treePath != "" {
				t.Fatalf("treePath = %q, want empty", treePath)
			}
			return WorkspaceTreeListing{
				Path: "",
				Items: []WorkspaceTreeEntry{
					{Name: "tmp", Path: "/tmp", IsDir: true, HasChildren: true},
				},
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/workspace/browse", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["path"] != "" {
		t.Fatalf("payload.path = %#v, want empty", payload["path"])
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("payload.items = %#v, want one item", payload["items"])
	}
}

func TestWorkspaceOpenRoutePost(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceOpen: func(_ context.Context, topicID string, targetPath string) error {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			if targetPath != "src/main.go" {
				t.Fatalf("targetPath = %q, want %q", targetPath, "src/main.go")
			}
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/workspace/open", strings.NewReader(`{"topic_id":"topic_a","path":"src/main.go"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload.ok = %#v, want true", payload["ok"])
	}
}

func TestFilesDownloadRouteGetWorkspaceDir(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	filePath := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceGet: func(_ context.Context, topicID string) (string, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return dir, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/files/download?dir_name=workspace_dir&topic_id=topic_a&path=src/main.go", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, `filename="main.go"`) {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if rec.Body.String() != "package main\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestFilesDownloadRouteGetStateDir(t *testing.T) {
	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
	})
	viper.Set("file_state_dir", stateDir)
	if err := os.WriteFile(filepath.Join(stateDir, "TODO.md"), []byte("# TODO\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
	})

	req := httptest.NewRequest(http.MethodGet, "/files/download?dir_name=file_state_dir&path=TODO.md", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "# TODO\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestFilesDownloadRouteGetCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	oldCacheDir := viper.GetString("file_cache_dir")
	t.Cleanup(func() {
		viper.Set("file_cache_dir", oldCacheDir)
	})
	viper.Set("file_cache_dir", cacheDir)
	if err := os.WriteFile(filepath.Join(cacheDir, "artifact.txt"), []byte("cached\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
	})

	req := httptest.NewRequest(http.MethodGet, "/files/download?dir_name=file_cache_dir&path=artifact.txt", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "cached\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestFilesDownloadRouteRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceGet: func(_ context.Context, _ string) (string, error) {
			return dir, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/files/download?dir_name=workspace_dir&topic_id=topic_a&path=src", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestFilesDownloadRouteRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceGet: func(_ context.Context, _ string) (string, error) {
			return dir, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/files/download?dir_name=workspace_dir&topic_id=topic_a&path=../secret.txt", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestFilesDownloadRouteRejectsMissingWorkspaceTopic(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceGet: func(_ context.Context, _ string) (string, error) {
			return t.TempDir(), nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/files/download?dir_name=workspace_dir&path=src/main.go", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestFilesPreviewRouteGetWorkspaceHTML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>Hello</h1>"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceGet: func(_ context.Context, topicID string) (string, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return dir, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/files/preview?dir_name=workspace_dir&topic_id=topic_a&path=index.html", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("Content-Disposition = %q, want empty", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "connect-src 'none'") {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if rec.Body.String() != "<h1>Hello</h1>" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestFilesPreviewRouteRejectsUnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.env"), []byte("TOKEN=x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceGet: func(_ context.Context, _ string) (string, error) {
			return dir, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/files/preview?dir_name=workspace_dir&topic_id=topic_a&path=secret.env", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestWorkspaceRouteUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
	})

	req := httptest.NewRequest(http.MethodGet, "/workspace?topic_id=topic_a", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestWorkspaceRouteBadRequestErrors(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspacePut: func(_ context.Context, topicID string, workspaceDir string) (string, error) {
			return "", BadRequest("workspace dir does not exist")
		},
	})

	req := httptest.NewRequest(http.MethodPut, "/workspace", strings.NewReader(`{"topic_id":"topic_a","workspace_dir":"./missing"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "workspace dir does not exist" {
		t.Fatalf("body = %q, want %q", got, "workspace dir does not exist")
	}
}
