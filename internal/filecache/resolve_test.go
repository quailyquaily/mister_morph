package filecache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveFile(t *testing.T) {
	cacheDir := t.TempDir()
	nestedDir := filepath.Join(cacheDir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	filePath := filepath.Join(nestedDir, "report.txt")
	if err := os.WriteFile(filePath, []byte("report"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	for _, test := range []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "relative path", path: "nested/report.txt"},
		{name: "cache alias", path: "file_cache_dir/nested/report.txt"},
		{name: "absolute path", path: filePath},
		{name: "directory", path: "nested", wantErr: "path is a directory"},
		{name: "file too large", path: "nested/report.txt", wantErr: "file too large to send"},
	} {
		t.Run(test.name, func(t *testing.T) {
			maxBytes := int64(1024)
			if test.name == "file too large" {
				maxBytes = 3
			}
			got, err := ResolveFile(cacheDir, test.path, maxBytes)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("ResolveFile() error = %v, want error containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveFile() error = %v", err)
			}
			if got != filePath {
				t.Fatalf("ResolveFile() path = %q, want %q", got, filePath)
			}
		})
	}
}

func TestResolveFileRequiresCacheDir(t *testing.T) {
	_, err := ResolveFile(" ", "report.txt", 1024)
	if err == nil || !strings.Contains(err.Error(), "file_cache_dir is required") {
		t.Fatalf("ResolveFile() error = %v, want required cache dir error", err)
	}
}

func TestResolveFileRequiresDirectoryCacheRoot(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(cacheFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ResolveFile(cacheFile, cacheFile, 1024)
	if err == nil || !strings.Contains(err.Error(), "file_cache_dir is not a directory") {
		t.Fatalf("ResolveFile() error = %v, want cache directory error", err)
	}
}

func TestResolveFileRejectsOutsideCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ResolveFile(cacheDir, outsidePath, 1024)
	if err == nil || !strings.Contains(err.Error(), "outside file_cache_dir") {
		t.Fatalf("ResolveFile() error = %v, want outside cache error", err)
	}
}

func TestResolveFileChecksResolvedSymlinkPath(t *testing.T) {
	cacheDir := t.TempDir()
	insidePath := filepath.Join(cacheDir, "inside.txt")
	if err := os.WriteFile(insidePath, []byte("inside"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	insideLink := filepath.Join(cacheDir, "inside-link.txt")
	escapeLink := filepath.Join(cacheDir, "escape-link.txt")
	if err := os.Symlink(insidePath, insideLink); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	if err := os.Symlink(outsidePath, escapeLink); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	got, err := ResolveFile(cacheDir, insideLink, 1024)
	if err != nil {
		t.Fatalf("ResolveFile() inside symlink error = %v", err)
	}
	if got != insidePath {
		t.Fatalf("ResolveFile() inside symlink path = %q, want %q", got, insidePath)
	}

	_, err = ResolveFile(cacheDir, escapeLink, 1024)
	if err == nil || !strings.Contains(err.Error(), "outside file_cache_dir") {
		t.Fatalf("ResolveFile() escape symlink error = %v, want outside cache error", err)
	}
}

func TestResolveFileResolvesSymlinkedCacheDir(t *testing.T) {
	realCacheDir := t.TempDir()
	filePath := filepath.Join(realCacheDir, "report.txt")
	if err := os.WriteFile(filePath, []byte("report"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cacheLink := filepath.Join(t.TempDir(), "cache")
	if err := os.Symlink(realCacheDir, cacheLink); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	got, err := ResolveFile(cacheLink, "report.txt", 1024)
	if err != nil {
		t.Fatalf("ResolveFile() error = %v", err)
	}
	if got != filePath {
		t.Fatalf("ResolveFile() path = %q, want %q", got, filePath)
	}
}
