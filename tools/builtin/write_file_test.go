package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileTool_RestrictedToBaseDir(t *testing.T) {
	base := t.TempDir()
	tool := NewWriteFileTool(true, 1024, base)

	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    "a.txt",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	b, err := os.ReadFile(filepath.Join(base, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("unexpected content: %q", string(b))
	}

	out, err = tool.Execute(context.Background(), map[string]any{
		"path":    filepath.Join(t.TempDir(), "outside.txt"),
		"content": "nope",
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "file_cache_dir") {
		t.Fatalf("expected error mentioning file_cache_dir, got %v", err)
	}
}

func TestWriteFileTool_PathTraversalRejected(t *testing.T) {
	base := t.TempDir()
	tool := NewWriteFileTool(true, 1024, base)

	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    "../escape.txt",
		"content": "nope",
		"mkdirs":  true,
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
}
