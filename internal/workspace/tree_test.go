package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAttachedTree(t *testing.T) {
	root := t.TempDir()
	alphaDir := filepath.Join(root, "alpha")
	betaDir := filepath.Join(root, "beta")
	gammaFile := filepath.Join(root, "gamma.txt")
	if err := os.Mkdir(alphaDir, 0o755); err != nil {
		t.Fatalf("os.Mkdir(alpha) error = %v", err)
	}
	if err := os.Mkdir(betaDir, 0o755); err != nil {
		t.Fatalf("os.Mkdir(beta) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(alphaDir, "nested.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(alpha/nested.txt) error = %v", err)
	}
	if err := os.WriteFile(gammaFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(gamma.txt) error = %v", err)
	}

	listing, err := ListAttachedTree(root, "")
	if err != nil {
		t.Fatalf("ListAttachedTree() error = %v", err)
	}
	if listing.RootPath != filepath.Clean(root) {
		t.Fatalf("listing.RootPath = %q, want %q", listing.RootPath, filepath.Clean(root))
	}
	if listing.Path != "" {
		t.Fatalf("listing.Path = %q, want empty", listing.Path)
	}
	if len(listing.Items) != 3 {
		t.Fatalf("len(listing.Items) = %d, want 3", len(listing.Items))
	}
	if listing.Items[0].Name != "alpha" || !listing.Items[0].IsDir || listing.Items[0].Path != "alpha" || !listing.Items[0].HasChildren {
		t.Fatalf("listing.Items[0] = %#v, want alpha dir with children", listing.Items[0])
	}
	if listing.Items[1].Name != "beta" || !listing.Items[1].IsDir || listing.Items[1].Path != "beta" || listing.Items[1].HasChildren {
		t.Fatalf("listing.Items[1] = %#v, want beta empty dir", listing.Items[1])
	}
	if listing.Items[1].SizeBytes < 0 {
		t.Fatalf("listing.Items[1].SizeBytes = %d, want non-negative", listing.Items[1].SizeBytes)
	}
	if listing.Items[2].Name != "gamma.txt" || listing.Items[2].IsDir || listing.Items[2].Path != "gamma.txt" {
		t.Fatalf("listing.Items[2] = %#v, want gamma.txt file", listing.Items[2])
	}
	if listing.Items[2].SizeBytes != 2 {
		t.Fatalf("listing.Items[2].SizeBytes = %d, want %d", listing.Items[2].SizeBytes, 2)
	}
}

func TestListAttachedTreeRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := ListAttachedTree(root, "../outside"); err == nil {
		t.Fatal("ListAttachedTree() error = nil, want escape rejection")
	}
}

func TestListSystemTree(t *testing.T) {
	root := t.TempDir()
	dirPath := filepath.Join(root, "docs")
	filePath := filepath.Join(root, "README.md")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatalf("os.Mkdir(docs) error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(README.md) error = %v", err)
	}

	listing, err := ListSystemTree(root)
	if err != nil {
		t.Fatalf("ListSystemTree() error = %v", err)
	}
	if listing.Path != filepath.Clean(root) {
		t.Fatalf("listing.Path = %q, want %q", listing.Path, filepath.Clean(root))
	}
	if len(listing.Items) != 2 {
		t.Fatalf("len(listing.Items) = %d, want 2", len(listing.Items))
	}
	if listing.Items[0].Path != filepath.Join(root, "docs") || !listing.Items[0].IsDir {
		t.Fatalf("listing.Items[0] = %#v, want docs dir", listing.Items[0])
	}
	if listing.Items[0].SizeBytes < 0 {
		t.Fatalf("listing.Items[0].SizeBytes = %d, want non-negative", listing.Items[0].SizeBytes)
	}
	if listing.Items[1].Path != filepath.Join(root, "README.md") || listing.Items[1].IsDir {
		t.Fatalf("listing.Items[1] = %#v, want README.md file", listing.Items[1])
	}
	if listing.Items[1].SizeBytes != 2 {
		t.Fatalf("listing.Items[1].SizeBytes = %d, want %d", listing.Items[1].SizeBytes, 2)
	}
}
