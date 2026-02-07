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
	if !strings.Contains(err.Error(), "allowed base dirs") {
		t.Fatalf("expected error mentioning allowed base dirs, got %v", err)
	}
}

func TestWriteFileTool_PathTraversalRejected(t *testing.T) {
	base := t.TempDir()
	tool := NewWriteFileTool(true, 1024, base)

	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    "../escape.txt",
		"content": "nope",
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
}

func TestWriteFileTool_AllowStateDirPrefix(t *testing.T) {
	cache := t.TempDir()
	state := t.TempDir()
	tool := NewWriteFileTool(true, 1024, cache, state)

	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    "file_state_dir/note.txt",
		"content": "ok",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}
	b, err := os.ReadFile(filepath.Join(state, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "ok" {
		t.Fatalf("unexpected content: %q", string(b))
	}
}

func TestWriteFileTool_BareAliasRejected(t *testing.T) {
	cache := t.TempDir()
	state := t.TempDir()
	tool := NewWriteFileTool(true, 1024, cache, state)

	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    "file_state_dir",
		"content": "nope",
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "alias requires a relative file path") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cache, "file_state_dir")); !os.IsNotExist(statErr) {
		t.Fatalf("unexpected file created under cache dir")
	}
}
