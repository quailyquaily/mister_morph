package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAttachedItemPath(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "notes.md")
	if err := os.WriteFile(targetPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(notes.md) error = %v", err)
	}

	resolved, err := ResolveAttachedItemPath(root, "notes.md")
	if err != nil {
		t.Fatalf("ResolveAttachedItemPath() error = %v", err)
	}
	if resolved != filepath.Clean(targetPath) {
		t.Fatalf("resolved = %q, want %q", resolved, filepath.Clean(targetPath))
	}
}

func TestResolveAttachedItemPathRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveAttachedItemPath(root, "../outside"); err == nil {
		t.Fatal("ResolveAttachedItemPath() error = nil, want escape rejection")
	}
}

func TestOpenPathUsesSystemRunner(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(targetPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(report.txt) error = %v", err)
	}

	previous := openPathRunner
	defer func() {
		openPathRunner = previous
	}()

	var opened string
	openPathRunner = func(path string) error {
		opened = path
		return nil
	}

	if err := OpenPath(targetPath); err != nil {
		t.Fatalf("OpenPath() error = %v", err)
	}
	if opened != filepath.Clean(targetPath) {
		t.Fatalf("opened = %q, want %q", opened, filepath.Clean(targetPath))
	}
}
