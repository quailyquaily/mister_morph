package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/filecache"
	"github.com/quailyquaily/mistermorph/internal/telegramutil"
)

func TestEnsureSecureCacheDir_FixesPerms(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := telegramutil.EnsureSecureCacheDir(dir); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("expected perms 0700, got %#o", fi.Mode().Perm())
	}
}

func TestEnsureSecureCacheDir_RejectsSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := telegramutil.EnsureSecureCacheDir(link); err == nil {
		t.Fatalf("expected error for symlink path, got nil")
	}
}

func TestCleanupFileCacheDir_MaxAgeAndMaxFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "telegram")
	if err := telegramutil.EnsureSecureCacheDir(dir); err != nil {
		t.Fatal(err)
	}

	old := filepath.Join(dir, "old.txt")
	mid := filepath.Join(dir, "mid.txt")
	newest := filepath.Join(dir, "new.txt")
	for _, p := range []string{old, mid, newest} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	_ = os.Chtimes(old, now.Add(-10*time.Hour), now.Add(-10*time.Hour))
	_ = os.Chtimes(mid, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	_ = os.Chtimes(newest, now.Add(-1*time.Minute), now.Add(-1*time.Minute))

	// Remove files older than 3h (old should go), then keep only 1 newest file.
	if err := filecache.Cleanup(dir, filecache.Limits{MaxAge: 3 * time.Hour, MaxFiles: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Fatalf("expected old file removed")
	}
	// Only one file should remain: newest.
	if _, err := os.Stat(newest); err != nil {
		t.Fatalf("expected newest file present, got %v", err)
	}
	if _, err := os.Stat(mid); err == nil {
		t.Fatalf("expected mid file removed due to max_files")
	}
}
