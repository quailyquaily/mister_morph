package imagesession

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
)

func TestStoreKeepsActiveImageByScope(t *testing.T) {
	cacheDir := t.TempDir()
	stateDir := t.TempDir()
	roots := pathroots.New("", cacheDir, stateDir)
	mustWriteImage(t, cacheDir, "images/a.png", "a")
	mustWriteImage(t, cacheDir, "images/b.png", "b")

	store := NewStore(stateDir)
	scopeA := NewScope("console:topic-a")
	scopeB := NewScope("console:topic-b")
	recA, err := store.Record(scopeA, roots, ImageRecord{Path: "file_cache_dir/images/a.png", MIMEType: "image/png"})
	if err != nil {
		t.Fatalf("record scope A: %v", err)
	}
	recB, err := store.Record(scopeB, roots, ImageRecord{Path: "file_cache_dir/images/b.png", MIMEType: "image/png"})
	if err != nil {
		t.Fatalf("record scope B: %v", err)
	}

	activeA, err := store.Active(scopeA, roots)
	if err != nil {
		t.Fatalf("active scope A: %v", err)
	}
	activeB, err := store.Active(scopeB, roots)
	if err != nil {
		t.Fatalf("active scope B: %v", err)
	}
	if activeA == nil || activeA.ID != recA.ID || activeA.Path != "file_cache_dir/images/a.png" {
		t.Fatalf("active A = %#v, want %q", activeA, recA.ID)
	}
	if activeB == nil || activeB.ID != recB.ID || activeB.Path != "file_cache_dir/images/b.png" {
		t.Fatalf("active B = %#v, want %q", activeB, recB.ID)
	}

	block, err := store.PromptBlock(scopeA, roots, 3)
	if err != nil {
		t.Fatalf("prompt block: %v", err)
	}
	if !strings.Contains(block.Content, recA.ID) || strings.Contains(block.Content, recB.ID) {
		t.Fatalf("prompt block content = %s", block.Content)
	}
}

func TestStoreClearsMissingActiveImage(t *testing.T) {
	cacheDir := t.TempDir()
	stateDir := t.TempDir()
	roots := pathroots.New("", cacheDir, stateDir)
	path := mustWriteImage(t, cacheDir, "images/missing.png", "x")

	store := NewStore(stateDir)
	scope := NewScope("tg:1")
	if _, err := store.Record(scope, roots, ImageRecord{Path: "file_cache_dir/images/missing.png"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove image: %v", err)
	}

	active, err := store.Active(scope, roots)
	if !errors.Is(err, ErrActiveImageMissing) {
		t.Fatalf("active err = %v, want ErrActiveImageMissing", err)
	}
	if active != nil {
		t.Fatalf("active = %#v, want nil", active)
	}
	block, err := store.PromptBlock(scope, roots, 3)
	if err != nil {
		t.Fatalf("prompt block: %v", err)
	}
	if strings.TrimSpace(block.Content) != "" {
		t.Fatalf("prompt block = %q, want empty", block.Content)
	}
}

func TestStoreProtectedPathsIncludesManifestFiles(t *testing.T) {
	cacheDir := t.TempDir()
	stateDir := t.TempDir()
	roots := pathroots.New("", cacheDir, stateDir)
	path := mustWriteImage(t, cacheDir, "images/keep.png", "x")

	store := NewStore(stateDir)
	if _, err := store.Record(NewScope("slack:T:C"), roots, ImageRecord{Path: "file_cache_dir/images/keep.png"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	protected, err := store.ProtectedPaths(cacheDir)
	if err != nil {
		t.Fatalf("protected paths: %v", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if !protected[filepath.Clean(abs)] {
		t.Fatalf("protected paths = %#v, want %s", protected, abs)
	}
}

func mustWriteImage(t *testing.T, cacheDir, rel, data string) string {
	t.Helper()
	path := filepath.Join(cacheDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}
	return path
}
