//go:build wailsdesktop

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildConsoleServeArgs(t *testing.T) {
	cfg := DesktopHostConfig{
		ConsoleBasePath: "console",
		ConfigPath:      "/tmp/morph.yaml",
		SetupMode:       true,
		SetupRequireLLM: true,
	}
	args := buildConsoleServeArgs(cfg, "127.0.0.1:12345", "/tmp/dist")
	want := []string{
		"console",
		"serve",
		"--console-listen", "127.0.0.1:12345",
		"--console-base-path", "/console",
		"--console-static-dir", "/tmp/dist",
		"--console-setup-mode=true",
		"--console-setup-require-llm=true",
		"--config", "/tmp/morph.yaml",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("buildConsoleServeArgs() mismatch\nwant: %#v\ngot : %#v", want, args)
	}
}

func TestExtractConfigPathFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "split form",
			args: []string{"--config", "/tmp/a.yaml"},
			want: "/tmp/a.yaml",
		},
		{
			name: "equals form",
			args: []string{"--config=/tmp/b.yaml"},
			want: "/tmp/b.yaml",
		},
		{
			name: "no config",
			args: []string{"--foo", "bar"},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractConfigPathFromArgs(tc.args)
			if got != tc.want {
				t.Fatalf("extractConfigPathFromArgs() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveConsoleStaticDir_Explicit(t *testing.T) {
	dir := t.TempDir()
	if err := writeTestFile(filepath.Join(dir, "index.html"), "<html></html>\n"); err != nil {
		t.Fatalf("prepare index: %v", err)
	}

	got, err := resolveConsoleStaticDir(dir)
	if err != nil {
		t.Fatalf("resolveConsoleStaticDir() error = %v", err)
	}
	if got != dir {
		t.Fatalf("resolveConsoleStaticDir() = %q, want %q", got, dir)
	}
}

func TestNormalizeConsoleBasePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "/console"},
		{"console", "/console"},
		{"/console", "/console"},
		{"/console/", "/console"},
	}
	for _, tc := range cases {
		if got := normalizeConsoleBasePath(tc.in); got != tc.want {
			t.Fatalf("normalizeConsoleBasePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func writeTestFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
