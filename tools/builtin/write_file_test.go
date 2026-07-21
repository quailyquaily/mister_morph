package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
)

func TestResolveWritePathRejectsSymlinkBase(t *testing.T) {
	realBase := t.TempDir()
	linkBase := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(realBase, linkBase); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, _, err := ResolveWritePath(pathroots.New(linkBase, "", ""), "notes.txt")
	if err == nil || !strings.Contains(err.Error(), "symlink base dir") {
		t.Fatalf("ResolveWritePath() error = %v, want symlink base error", err)
	}
}

func TestResolveWritePathRejectsFileBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(base, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, _, err := ResolveWritePath(pathroots.New(base, "", ""), "notes.txt")
	if err == nil {
		t.Fatal("ResolveWritePath() error = nil, want file base error")
	}
}

func TestResolveWritePathNormalizesBaseDirectoryMode(t *testing.T) {
	base := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	gotBase, gotPath, err := ResolveWritePath(pathroots.New(base, "", ""), "notes.txt")
	if err != nil {
		t.Fatalf("ResolveWritePath() error = %v", err)
	}
	info, err := os.Stat(gotBase)
	if err != nil {
		t.Fatalf("Stat(base) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("base mode = %o, want 700", got)
	}
	if gotPath != filepath.Join(gotBase, "notes.txt") {
		t.Fatalf("resolved path = %q, want %q", gotPath, filepath.Join(gotBase, "notes.txt"))
	}
}

func TestResolveWritePathReturnsCanonicalValidationErrors(t *testing.T) {
	tests := []struct {
		name  string
		roots pathroots.PathRoots
		path  string
		want  string
	}{
		{name: "missing roots", path: "notes.txt", want: "is not configured"},
		{name: "empty path", roots: pathroots.New(t.TempDir(), "", ""), path: "  ", want: "missing required param"},
		{name: "missing alias base", roots: pathroots.New(t.TempDir(), "", ""), path: "file_state_dir/notes.txt", want: "is not configured"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ResolveWritePath(tt.roots, tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ResolveWritePath() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestWriteFileTool_RestrictedToBaseDir(t *testing.T) {
	base := t.TempDir()
	tool := NewWriteFileTool(true, 1024, pathroots.New("", base, ""))

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
	tool := NewWriteFileTool(true, 1024, pathroots.New("", base, ""))

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
	tool := NewWriteFileTool(true, 1024, pathroots.New("", cache, state))

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
	tool := NewWriteFileTool(true, 1024, pathroots.New("", cache, state))

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

func TestWriteFileTool_RelativePathUsesWorkspaceDirFromContext(t *testing.T) {
	workspaceDir := t.TempDir()
	cacheDir := t.TempDir()
	stateDir := t.TempDir()
	tool := NewWriteFileTool(true, 1024, pathroots.New("", cacheDir, stateDir))

	ctx := pathroots.WithWorkspaceDir(context.Background(), workspaceDir)
	out, err := tool.Execute(ctx, map[string]any{
		"path":    "note.txt",
		"content": "workspace",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	got, err := os.ReadFile(filepath.Join(workspaceDir, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "workspace" {
		t.Fatalf("unexpected content: %q", string(got))
	}
}
