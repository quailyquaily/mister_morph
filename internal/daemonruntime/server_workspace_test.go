package daemonruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/spf13/viper"
)

type testUploadFile struct {
	name string
	body string
}

func newFilesUploadRequest(t *testing.T, fields map[string]string, files []testUploadFile) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("WriteField(%q) error = %v", name, err)
		}
	}
	for _, file := range files {
		part, err := writer.CreateFormFile("files", file.name)
		if err != nil {
			t.Fatalf("CreateFormFile(%q) error = %v", file.name, err)
		}
		if _, err := part.Write([]byte(file.body)); err != nil {
			t.Fatalf("Write(%q) error = %v", file.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart.Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/files/upload", &body)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func decodeFilesUploadResponse(t *testing.T, rec *httptest.ResponseRecorder) []struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	DirName   string `json:"dir_name"`
	SizeBytes int64  `json:"size_bytes"`
} {
	t.Helper()
	var payload struct {
		Files []struct {
			Name      string `json:"name"`
			Path      string `json:"path"`
			DirName   string `json:"dir_name"`
			SizeBytes int64  `json:"size_bytes"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return payload.Files
}

func TestWorkspaceRouteGet(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		Workspace: WorkspaceRoutes{
			Get: func(_ context.Context, topicID string) (WorkspaceResolution, error) {
				if topicID != "topic_a" {
					t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
				}
				return WorkspaceResolution{WorkspaceDir: "/repo/project", Source: "default"}, nil
			},
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
	if payload["source"] != "default" {
		t.Fatalf("payload.source = %#v, want default", payload["source"])
	}
}

func TestWorkspaceRoutePut(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{Put: func(_ context.Context, topicID string, workspaceDir string) (WorkspaceResolution, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			if workspaceDir != "./repo" {
				t.Fatalf("workspaceDir = %q, want %q", workspaceDir, "./repo")
			}
			return WorkspaceResolution{WorkspaceDir: "/repo/project", Source: "attachment"}, nil
		}},
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
	if payload["source"] != "attachment" {
		t.Fatalf("payload.source = %#v, want attachment", payload["source"])
	}
}

func TestWorkspaceRouteDelete(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{Delete: func(_ context.Context, topicID string) (WorkspaceResolution, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return WorkspaceResolution{WorkspaceDir: "/srv/default", Source: "default"}, nil
		}},
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
	if payload["workspace_dir"] != "/srv/default" {
		t.Fatalf("payload.workspace_dir = %#v, want /srv/default", payload["workspace_dir"])
	}
	if payload["source"] != "default" {
		t.Fatalf("payload.source = %#v, want default", payload["source"])
	}
}

func TestWorkspaceTreeRouteGet(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{Tree: func(_ context.Context, topicID string, treePath string) (WorkspaceTreeListing, error) {
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
		}},
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
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	paths := testRuntimePaths(stateDir)
	paths.CacheDir = cacheDir

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "console",
		AuthToken:    "token",
		RuntimePaths: paths, Workspace: WorkspaceRoutes{Browse: func(_ context.Context, treePath string, showHidden bool) (WorkspaceTreeListing, error) {
			if treePath != "/tmp" {
				t.Fatalf("treePath = %q, want /tmp", treePath)
			}
			if !showHidden {
				t.Fatalf("showHidden = false, want true")
			}
			return WorkspaceTreeListing{
				Path: "/tmp",
				Items: []WorkspaceTreeEntry{
					{Name: "tmp", Path: "/tmp", IsDir: true, HasChildren: true},
				},
			}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/workspace/browse?path=%2Ftmp&show_hidden=true", nil)
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
	if payload["path"] != "/tmp" {
		t.Fatalf("payload.path = %#v, want /tmp", payload["path"])
	}
	if payload["state_dir"] != filepath.Clean(stateDir) {
		t.Fatalf("payload.state_dir = %#v, want %q", payload["state_dir"], filepath.Clean(stateDir))
	}
	if payload["cache_dir"] != filepath.Clean(cacheDir) {
		t.Fatalf("payload.cache_dir = %#v, want %q", payload["cache_dir"], filepath.Clean(cacheDir))
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("payload.items = %#v, want one item", payload["items"])
	}
}

func TestWorkspaceDirectoryRoutePost(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{CreateDir: func(_ context.Context, parentPath string, name string) (string, error) {
			if parentPath != "/repo" {
				t.Fatalf("parentPath = %q, want /repo", parentPath)
			}
			if name != "new workspace" {
				t.Fatalf("name = %q, want %q", name, "new workspace")
			}
			return "/repo/new workspace", nil
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/workspace/directory", strings.NewReader(`{"parent_path":"/repo","name":"new workspace"}`))
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
	if payload["path"] != "/repo/new workspace" {
		t.Fatalf("payload.path = %#v, want %q", payload["path"], "/repo/new workspace")
	}
}

func TestWorkspaceDirectoryRoutePostRequiresName(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{CreateDir: func(context.Context, string, string) (string, error) { return "", nil }},
	})

	req := httptest.NewRequest(http.MethodPost, "/workspace/directory", strings.NewReader(`{"parent_path":"/repo"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestFilesUploadRouteUsesAttachedWorkspace(t *testing.T) {
	workspaceDir := t.TempDir()
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, topicID string) (WorkspaceResolution, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want topic_a", topicID)
			}
			return WorkspaceResolution{WorkspaceDir: workspaceDir, Source: "attachment"}, nil
		}},
	})

	req := newFilesUploadRequest(t, map[string]string{"topic_id": "topic_a"}, []testUploadFile{
		{name: "notes.txt", body: "first\n"},
		{name: "diagram.svg", body: "<svg></svg>"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	files := decodeFilesUploadResponse(t, rec)
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	if files[0].Name != "notes.txt" || files[0].Path != "notes.txt" || files[0].DirName != "workspace_dir" || files[0].SizeBytes != 6 {
		t.Fatalf("files[0] = %#v", files[0])
	}
	if strings.Contains(rec.Body.String(), `"reference"`) {
		t.Fatalf("upload response contains redundant reference field: %s", rec.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(workspaceDir, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(raw) != "first\n" {
		t.Fatalf("uploaded body = %q, want first\\n", string(raw))
	}
}

func TestFilesUploadRouteFallsBackToCacheWithoutAttachedWorkspace(t *testing.T) {
	cacheDir := t.TempDir()

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "console",
		AuthToken:    "token",
		RuntimePaths: runtimepaths.Paths{CacheDir: cacheDir}, Workspace: WorkspaceRoutes{Get: func(_ context.Context, _ string) (WorkspaceResolution, error) {
			return WorkspaceResolution{Source: "none"}, nil
		}},
	})

	req := newFilesUploadRequest(t, map[string]string{"topic_id": "topic_a"}, []testUploadFile{
		{name: "report.pdf", body: "pdf-data"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	files := decodeFilesUploadResponse(t, rec)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	if files[0].Path != "console/report.pdf" || files[0].DirName != "file_cache_dir" {
		t.Fatalf("files[0] = %#v", files[0])
	}
	if raw, err := os.ReadFile(filepath.Join(cacheDir, "console", "report.pdf")); err != nil || string(raw) != "pdf-data" {
		t.Fatalf("cache upload body = %q, error = %v", string(raw), err)
	}
}

func TestFilesUploadRouteUsesDefaultWorkspaceForNewTopic(t *testing.T) {
	workspaceDir := t.TempDir()
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		Workspace: WorkspaceRoutes{DefaultDir: workspaceDir},
	})

	req := newFilesUploadRequest(t, nil, []testUploadFile{{name: "brief.md", body: "# Brief\n"}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	files := decodeFilesUploadResponse(t, rec)
	if len(files) != 1 || files[0].Path != "brief.md" || files[0].DirName != "workspace_dir" {
		t.Fatalf("files = %#v", files)
	}
	if raw, err := os.ReadFile(filepath.Join(workspaceDir, "brief.md")); err != nil || string(raw) != "# Brief\n" {
		t.Fatalf("default workspace upload body = %q, error = %v", string(raw), err)
	}
}

func TestFilesUploadRouteRollsBackBatchOnFailure(t *testing.T) {
	cacheDir := t.TempDir()
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "console",
		AuthToken:    "token",
		RuntimePaths: runtimepaths.Paths{CacheDir: cacheDir},
	})

	req := newFilesUploadRequest(t, nil, []testUploadFile{
		{name: "created-before-error.txt", body: "must be removed"},
		{name: "..", body: "invalid name"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(cacheDir, "console"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache entries after failed batch = %v, want none", entries)
	}
}

func TestFilesUploadRouteCleansExpiredConsoleCacheFiles(t *testing.T) {
	cacheDir := t.TempDir()
	consoleCacheDir := filepath.Join(cacheDir, "console")
	if err := os.MkdirAll(consoleCacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	expiredPath := filepath.Join(consoleCacheDir, "expired.txt")
	if err := os.WriteFile(expiredPath, []byte("expired"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	expiredAt := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(expiredPath, expiredAt, expiredAt); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "console",
		AuthToken:    "token",
		RuntimePaths: runtimepaths.Paths{CacheDir: cacheDir},
	})
	req := newFilesUploadRequest(t, nil, []testUploadFile{{name: "current.txt", body: "current"}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
		t.Fatalf("expired cache file Stat() error = %v, want not exist", err)
	}
	if raw, err := os.ReadFile(filepath.Join(consoleCacheDir, "current.txt")); err != nil || string(raw) != "current" {
		t.Fatalf("current cache body = %q, error = %v", string(raw), err)
	}
}

func TestFilesUploadRouteUsesCapturedConsoleCacheLimits(t *testing.T) {
	cacheDir := t.TempDir()
	consoleCacheDir := filepath.Join(cacheDir, "console")
	if err := os.MkdirAll(consoleCacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	oldPath := filepath.Join(consoleCacheDir, "old.txt")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	settings := viper.New()
	settings.Set("file_cache.max_age", 24*time.Hour)
	settings.Set("file_cache.max_files", 1)
	settings.Set("file_cache.max_total_bytes", int64(1024))

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:                "console",
		AuthToken:           "token",
		RuntimePaths:        runtimepaths.Paths{CacheDir: cacheDir},
		AgentSettingsReader: settings,
	})
	settings.Set("file_cache.max_files", 100)
	req := newFilesUploadRequest(t, nil, []testUploadFile{{name: "current.txt", body: "current"}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old cache file Stat() error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(consoleCacheDir, "current.txt")); err != nil {
		t.Fatalf("current cache file Stat() error = %v", err)
	}
}

func TestFilesUploadRouteUsesPendingWorkspace(t *testing.T) {
	workspaceDir := t.TempDir()
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, _ string) (WorkspaceResolution, error) {
			t.Fatal("WorkspaceGet must not be called for a pending workspace")
			return WorkspaceResolution{}, nil
		}},
	})

	req := newFilesUploadRequest(t, map[string]string{"workspace_dir": workspaceDir}, []testUploadFile{
		{name: "brief.md", body: "# Brief\n"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	files := decodeFilesUploadResponse(t, rec)
	if len(files) != 1 || files[0].Path != "brief.md" || files[0].DirName != "workspace_dir" {
		t.Fatalf("files = %#v", files)
	}
	if raw, err := os.ReadFile(filepath.Join(workspaceDir, "brief.md")); err != nil || string(raw) != "# Brief\n" {
		t.Fatalf("workspace upload body = %q, error = %v", string(raw), err)
	}
}

func TestFilesUploadRouteKeepsExistingFile(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, "notes.txt"), []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, _ string) (WorkspaceResolution, error) {
			return WorkspaceResolution{WorkspaceDir: workspaceDir, Source: "attachment"}, nil
		}},
	})

	req := newFilesUploadRequest(t, map[string]string{"topic_id": "topic_a"}, []testUploadFile{
		{name: "notes.txt", body: "new\n"},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	files := decodeFilesUploadResponse(t, rec)
	if len(files) != 1 || files[0].Name != "notes (1).txt" || files[0].Path != "notes (1).txt" || files[0].DirName != "workspace_dir" {
		t.Fatalf("files = %#v", files)
	}
	if raw, err := os.ReadFile(filepath.Join(workspaceDir, "notes.txt")); err != nil || string(raw) != "existing\n" {
		t.Fatalf("existing body = %q, error = %v", string(raw), err)
	}
	if raw, err := os.ReadFile(filepath.Join(workspaceDir, "notes (1).txt")); err != nil || string(raw) != "new\n" {
		t.Fatalf("uploaded body = %q, error = %v", string(raw), err)
	}
}

func TestFilesUploadRouteRequiresFiles(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{Mode: "console", AuthToken: "token"})
	req := newFilesUploadRequest(t, nil, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestWorkspaceOpenRoutePost(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{Open: func(_ context.Context, topicID string, targetPath string) error {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			if targetPath != "src/main.go" {
				t.Fatalf("targetPath = %q, want %q", targetPath, "src/main.go")
			}
			return nil
		}},
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
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, topicID string) (WorkspaceResolution, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return WorkspaceResolution{WorkspaceDir: dir, Source: "attachment"}, nil
		}},
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
	if err := os.WriteFile(filepath.Join(stateDir, "TODO.md"), []byte("# TODO\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "console",
		AuthToken:    "token",
		RuntimePaths: testRuntimePaths(stateDir),
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
	if err := os.WriteFile(filepath.Join(cacheDir, "artifact.txt"), []byte("cached\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "console",
		AuthToken:    "token",
		RuntimePaths: runtimepaths.Paths{CacheDir: cacheDir},
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
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, _ string) (WorkspaceResolution, error) {
			return WorkspaceResolution{WorkspaceDir: dir, Source: "attachment"}, nil
		}},
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
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, _ string) (WorkspaceResolution, error) {
			return WorkspaceResolution{WorkspaceDir: dir, Source: "attachment"}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/files/download?dir_name=workspace_dir&topic_id=topic_a&path=../secret.txt", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestFilesDownloadRouteRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	linkPath := filepath.Join(dir, "secret.txt")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, _ string) (WorkspaceResolution, error) {
			return WorkspaceResolution{WorkspaceDir: dir, Source: "attachment"}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/files/download?dir_name=workspace_dir&topic_id=topic_a&path=secret.txt", nil)
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
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, _ string) (WorkspaceResolution, error) {
			return WorkspaceResolution{WorkspaceDir: t.TempDir(), Source: "attachment"}, nil
		}},
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
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, topicID string) (WorkspaceResolution, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return WorkspaceResolution{WorkspaceDir: dir, Source: "attachment"}, nil
		}},
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

func TestFilesPreviewRouteRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.html")
	if err := os.WriteFile(outsidePath, []byte("<h1>secret</h1>"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	linkPath := filepath.Join(dir, "secret.html")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, _ string) (WorkspaceResolution, error) {
			return WorkspaceResolution{WorkspaceDir: dir, Source: "attachment"}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/files/preview?dir_name=workspace_dir&topic_id=topic_a&path=secret.html", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
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
		AuthToken: "token", Workspace: WorkspaceRoutes{Get: func(_ context.Context, _ string) (WorkspaceResolution, error) {
			return WorkspaceResolution{WorkspaceDir: dir, Source: "attachment"}, nil
		}},
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
		AuthToken: "token", Workspace: WorkspaceRoutes{Put: func(_ context.Context, topicID string, workspaceDir string) (WorkspaceResolution, error) {
			return WorkspaceResolution{}, BadRequest("workspace dir does not exist")
		}},
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
