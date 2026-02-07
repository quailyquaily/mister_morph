package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDenyPath(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		deny     []string
		wantDeny bool
	}{
		{name: "basename_exact", path: "config.yaml", deny: []string{"config.yaml"}, wantDeny: true},
		{name: "basename_nested", path: "./sub/config.yaml", deny: []string{"config.yaml"}, wantDeny: true},
		{name: "basename_other", path: "./sub/config.yml", deny: []string{"config.yaml"}, wantDeny: false},
		{name: "basename_suffix_not_match", path: "config.yaml.bak", deny: []string{"config.yaml"}, wantDeny: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := denyPath(tc.path, tc.deny)
			if got != tc.wantDeny {
				t.Fatalf("denyPath(%q,%v)=%v, want %v", tc.path, tc.deny, got, tc.wantDeny)
			}
		})
	}
}

func TestReadFileTool_ExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.WriteFile(filepath.Join(home, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewReadFileTool(1024)
	out, err := tool.Execute(context.Background(), map[string]any{"path": "~/hello.txt"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if out != "hi" {
		t.Fatalf("got %q, want %q", out, "hi")
	}
}

func TestWriteFileTool_ExpandsTildeInBaseDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tool := NewWriteFileTool(true, 1024, "~/.morph-cache")
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":    "out.txt",
		"content": "ok",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(home, ".morph-cache", "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" {
		t.Fatalf("got %q, want %q", string(got), "ok")
	}
}

func TestReadFileTool_AliasToStateDir(t *testing.T) {
	cache := t.TempDir()
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewReadFileToolWithDenyPaths(1024, nil, cache, state)
	out, err := tool.Execute(context.Background(), map[string]any{"path": "file_state_dir/note.txt"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if out != "hi" {
		t.Fatalf("got %q, want %q", out, "hi")
	}
}

func TestReadFileTool_BareAliasRejected(t *testing.T) {
	cache := t.TempDir()
	state := t.TempDir()
	tool := NewReadFileToolWithDenyPaths(1024, nil, cache, state)
	_, err := tool.Execute(context.Background(), map[string]any{"path": "file_state_dir"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "alias requires a relative file path") {
		t.Fatalf("unexpected error: %v", err)
	}
}
