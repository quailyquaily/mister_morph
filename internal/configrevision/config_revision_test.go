package configrevision

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTracksRawFileChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	first := []byte("llm:\n  model: first\n")
	if err := os.WriteFile(path, first, 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision == "" {
		t.Fatal("revision is empty")
	}
	if string(snapshot.Data) != string(first) {
		t.Fatalf("data = %q, want %q", snapshot.Data, first)
	}

	if err := os.WriteFile(path, []byte("llm:\n  model: second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision == snapshot.Revision {
		t.Fatalf("revision did not change: %q", changed.Revision)
	}
}

func TestReadMissingFileHasStableRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	first, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision == "" || first.Revision != second.Revision {
		t.Fatalf("missing file revisions = %q and %q", first.Revision, second.Revision)
	}
}
