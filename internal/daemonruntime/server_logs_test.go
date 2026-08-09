package daemonruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogsLatestRoutePaginatesAcrossFiles(t *testing.T) {
	stateDir := t.TempDir()
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(logDir) error = %v", err)
	}
	writeLogFixture(t, logDir, "mistermorph-2026-04-23.jsonl", []string{`{"msg":"old-1"}`, `{"msg":"old-2"}`})
	writeLogFixture(t, logDir, "mistermorph-2026-04-24.jsonl", []string{`{"msg":"new-1"}`, `{"msg":"new-2"}`, `{"msg":"new-3"}`, `{"msg":"new-4"}`})

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{AuthToken: "token", RuntimePaths: testRuntimePaths(stateDir)})

	first := requestLogChunk(t, mux, "/logs/latest?limit=2", "token")
	if first.File != "mistermorph-2026-04-24.jsonl" {
		t.Fatalf("first.File = %q, want latest file", first.File)
	}
	if strings.Join(first.Items, "\n") != `{"msg":"new-3"}`+"\n"+`{"msg":"new-4"}` {
		t.Fatalf("first lines = %#v", first.Items)
	}
	if !first.HasNext || first.NextCursor == "" {
		t.Fatalf("first page missing next cursor: %+v", first)
	}

	second := requestLogChunk(t, mux, "/logs/latest?limit=2&cursor="+first.NextCursor, "token")
	if second.File != "mistermorph-2026-04-24.jsonl" {
		t.Fatalf("second.File = %q, want same file", second.File)
	}
	if strings.Join(second.Items, "\n") != `{"msg":"new-1"}`+"\n"+`{"msg":"new-2"}` {
		t.Fatalf("second lines = %#v", second.Items)
	}
	if !second.HasNext || second.NextCursor == "" {
		t.Fatalf("second page missing cross-file cursor: %+v", second)
	}

	third := requestLogChunk(t, mux, "/logs/latest?limit=2&cursor="+second.NextCursor, "token")
	if third.File != "mistermorph-2026-04-23.jsonl" {
		t.Fatalf("third.File = %q, want previous file", third.File)
	}
	if strings.Join(third.Items, "\n") != `{"msg":"old-1"}`+"\n"+`{"msg":"old-2"}` {
		t.Fatalf("third lines = %#v", third.Items)
	}
	if third.HasNext || third.NextCursor != "" {
		t.Fatalf("third page should be oldest: %+v", third)
	}
}

func TestLogsLatestRouteDoesNotExposeAbsolutePath(t *testing.T) {
	stateDir := t.TempDir()
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(logDir) error = %v", err)
	}
	writeLogFixture(t, logDir, "mistermorph-2026-04-24.jsonl", []string{`{"msg":"hello"}`})

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{AuthToken: "token", RuntimePaths: testRuntimePaths(stateDir)})

	req := httptest.NewRequest(http.MethodGet, "/logs/latest", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), stateDir) || strings.Contains(rec.Body.String(), logDir) {
		t.Fatalf("response exposed absolute path: %s", rec.Body.String())
	}
}

func TestLogsLatestRouteRequiresAuth(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{AuthToken: "token"})

	req := httptest.NewRequest(http.MethodGet, "/logs/latest", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLogsLatestRouteEmptyWhenNoLogFiles(t *testing.T) {
	stateDir := t.TempDir()

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{AuthToken: "token", RuntimePaths: testRuntimePaths(stateDir)})

	chunk := requestLogChunk(t, mux, "/logs/latest", "token")
	if chunk.Exists || len(chunk.Items) != 0 {
		t.Fatalf("chunk = %+v, want empty missing log state", chunk)
	}
}

func requestLogChunk(t *testing.T, mux http.Handler, path string, token string) logChunk {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d, want %d (%s)", path, rec.Code, http.StatusOK, rec.Body.String())
	}
	var chunk logChunk
	if err := json.Unmarshal(rec.Body.Bytes(), &chunk); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return chunk
}

func writeLogFixture(t *testing.T, dir string, name string, lines []string) {
	t.Helper()
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
}
