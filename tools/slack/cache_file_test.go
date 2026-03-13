package slack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveFileCachePath(t *testing.T) {
	cacheDir := t.TempDir()
	filePath := filepath.Join(cacheDir, "nested", "report.txt")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte("ok"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := resolveFileCachePath(cacheDir, "file_cache_dir/nested/report.txt", 1024)
	if err != nil {
		t.Fatalf("resolveFileCachePath() error = %v", err)
	}
	if got != filePath {
		t.Fatalf("path = %q, want %q", got, filePath)
	}
}

func TestResolveFileCachePathRejectsDirectory(t *testing.T) {
	cacheDir := t.TempDir()
	dirPath := filepath.Join(cacheDir, "nested")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	_, err := resolveFileCachePath(cacheDir, "nested", 1024)
	if err == nil {
		t.Fatalf("resolveFileCachePath() error = nil, want directory error")
	}
	if !strings.Contains(err.Error(), "path is a directory") {
		t.Fatalf("error = %v, want directory error", err)
	}
}

func TestResolveFileCachePathRejectsOutsideCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "report.txt")
	if err := os.WriteFile(outsidePath, []byte("ok"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := resolveFileCachePath(cacheDir, outsidePath, 1024)
	if err == nil {
		t.Fatalf("resolveFileCachePath() error = nil, want outside-file error")
	}
	if !strings.Contains(err.Error(), "outside file_cache_dir") {
		t.Fatalf("error = %v, want outside file_cache_dir", err)
	}
}

func TestResolveFileCachePathRejectsTooLargeFile(t *testing.T) {
	cacheDir := t.TempDir()
	filePath := filepath.Join(cacheDir, "big.bin")
	if err := os.WriteFile(filePath, []byte("12345"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := resolveFileCachePath(cacheDir, "big.bin", 4)
	if err == nil {
		t.Fatalf("resolveFileCachePath() error = nil, want size error")
	}
	if !strings.Contains(err.Error(), "file too large to send") {
		t.Fatalf("error = %v, want size error", err)
	}
}

func TestResolveFileCachePathRejectsSymlinkEscapingCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	linkPath := filepath.Join(cacheDir, "escape.txt")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	_, err := resolveFileCachePath(cacheDir, "escape.txt", 1024)
	if err == nil {
		t.Fatalf("resolveFileCachePath() error = nil, want outside-file error")
	}
	if !strings.Contains(err.Error(), "outside file_cache_dir") {
		t.Fatalf("error = %v, want outside file_cache_dir", err)
	}
}
