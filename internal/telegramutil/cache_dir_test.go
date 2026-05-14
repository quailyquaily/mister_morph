package telegramutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupFileCacheDirWithProtectedKeepsReferencedFiles(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	keep := filepath.Join(dir, "keep.txt")
	newest := filepath.Join(dir, "new.txt")
	for _, path := range []string{old, keep, newest} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	_ = os.Chtimes(old, now.Add(-10*time.Hour), now.Add(-10*time.Hour))
	_ = os.Chtimes(keep, now.Add(-9*time.Hour), now.Add(-9*time.Hour))
	_ = os.Chtimes(newest, now.Add(-1*time.Minute), now.Add(-1*time.Minute))

	protected := map[string]bool{keep: true}
	if err := CleanupFileCacheDirWithProtected(dir, 3*time.Hour, 1, 0, protected); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("expected protected file present, got %v", err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Fatalf("expected old unprotected file removed")
	}
	if _, err := os.Stat(newest); err == nil {
		t.Fatalf("expected newest unprotected file removed by max_files")
	}
}
