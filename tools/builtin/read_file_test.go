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

func TestContainsDotDot(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"../../etc/passwd", true},
		{"/var/cache/../../../etc/passwd", true},
		{"/etc/passwd", false},
		{"foo/bar", false},
		{"..hidden", false},
		{"foo/..bar/baz", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := containsDotDot(tc.path)
			if got != tc.want {
				t.Fatalf("containsDotDot(%q)=%v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestReadFileTool_PathTraversalRejected(t *testing.T) {
	tool := NewReadFileTool(1024)

	cases := []struct {
		name string
		path string
	}{
		{"dot_dot_relative", "../../etc/passwd"},
		{"dot_dot_absolute", "/var/cache/../../../etc/passwd"},
		{"dot_dot_mid_path", "/some/dir/../../../etc/shadow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tool.Execute(context.Background(), map[string]any{"path": tc.path})
			if err == nil {
				t.Fatalf("expected error for path %q, got nil (out=%q)", tc.path, out)
			}
			if !strings.Contains(err.Error(), "traversal") {
				t.Fatalf("expected path traversal error, got: %v", err)
			}
		})
	}
}

func TestReadFileTool_AllowedDirs(t *testing.T) {
	allowedDir := t.TempDir()
	outsideDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(allowedDir, "ok.txt"), []byte("allowed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "nope.txt"), []byte("denied"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewReadFileToolWithOptions(1024, nil, []string{allowedDir})

	out, err := tool.Execute(context.Background(), map[string]any{"path": filepath.Join(allowedDir, "ok.txt")})
	if err != nil {
		t.Fatalf("expected nil error for allowed path, got %v", err)
	}
	if out != "allowed" {
		t.Fatalf("got %q, want %q", out, "allowed")
	}

	out, err = tool.Execute(context.Background(), map[string]any{"path": filepath.Join(outsideDir, "nope.txt")})
	if err == nil {
		t.Fatalf("expected error for path outside allowed_dirs, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "not within any allowed directory") {
		t.Fatalf("expected allowed directory error, got: %v", err)
	}
}

func TestReadFile_SymlinkReject(t *testing.T) {
	allowedDir := t.TempDir()
	outsideDir := t.TempDir()

	// Create a real file outside allowed_dirs.
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside allowed_dirs pointing outside.
	symlinkPath := filepath.Join(allowedDir, "escape_link")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatal(err)
	}

	tool := NewReadFileToolWithOptions(1024, nil, []string{allowedDir})

	// Symlink should be rejected even though it's "inside" allowedDir.
	out, err := tool.Execute(context.Background(), map[string]any{"path": symlinkPath})
	if err == nil {
		t.Fatalf("expected error for symlink, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink error, got: %v", err)
	}

	// Regular file inside allowed_dirs should still work.
	regularFile := filepath.Join(allowedDir, "ok.txt")
	if err := os.WriteFile(regularFile, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = tool.Execute(context.Background(), map[string]any{"path": regularFile})
	if err != nil {
		t.Fatalf("expected nil error for regular file, got: %v", err)
	}
	if out != "ok" {
		t.Fatalf("got %q, want %q", out, "ok")
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
