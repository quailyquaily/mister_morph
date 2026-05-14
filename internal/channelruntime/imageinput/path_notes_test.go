package imageinput

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendImagePathNotesUsesFileCacheAlias(t *testing.T) {
	cacheDir := t.TempDir()
	imagePath := filepath.Join(cacheDir, "telegram", "a.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := AppendImagePathNotes("edit this", []string{imagePath}, cacheDir)
	if !strings.Contains(got, "edit this") {
		t.Fatalf("content missing: %q", got)
	}
	if !strings.Contains(got, "file_cache_dir/telegram/a.png") {
		t.Fatalf("alias path missing: %q", got)
	}
	if strings.Contains(got, cacheDir) {
		t.Fatalf("local cache path leaked: %q", got)
	}
}
