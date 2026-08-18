//go:build wailsdesktop

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildConsoleServeArgs(t *testing.T) {
	cfg := DesktopHostConfig{
		ConsoleBasePath: "console",
		ConfigPath:      "/tmp/morph.yaml",
	}
	args := buildConsoleServeArgs([]string{"console", "serve"}, cfg, "127.0.0.1:12345")
	want := []string{
		"console",
		"serve",
		"--console-listen", "127.0.0.1:12345",
		"--console-base-path", "/console",
		"--allow-empty-password",
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
			want: filepath.Clean("/tmp/a.yaml"),
		},
		{
			name: "equals form",
			args: []string{"--config=/tmp/b.yaml"},
			want: filepath.Clean("/tmp/b.yaml"),
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

func TestNormalizeConsoleBasePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "/"},
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

func TestSameExecutablePath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "mistermorph")
	if err := os.WriteFile(a, []byte("x"), 0o755); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if !sameExecutablePath(a, a) {
		t.Fatalf("same path should be true")
	}
	if sameExecutablePath(a, filepath.Join(dir, "other")) {
		t.Fatalf("different path should be false")
	}
}

func TestBuildDesktopChildEnvLeavesNormalEnvUntouched(t *testing.T) {
	base := []string{
		"HOME=/tmp/home",
		"PATH=/usr/bin",
	}
	got := buildDesktopChildEnv(base)
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("buildDesktopChildEnv() = %#v, want %#v", got, base)
	}
}

func TestBuildDesktopChildEnvSanitizesAppImageEnv(t *testing.T) {
	base := []string{
		"HOME=/tmp/home",
		"APPIMAGE=/tmp/MisterMorph.AppImage",
		"APPDIR=/tmp/.mount_MisterMorph",
		"ARGV0=/tmp/MisterMorph.AppImage",
		"OWD=/tmp",
		"LD_LIBRARY_PATH=/tmp/.mount_MisterMorph/usr/lib",
		"LD_PRELOAD=/tmp/libhack.so",
		"PATH=/usr/bin",
	}
	got := buildDesktopChildEnv(base)

	for _, blocked := range []string{"APPIMAGE=", "APPDIR=", "ARGV0=", "OWD=", "LD_LIBRARY_PATH=", "LD_PRELOAD="} {
		for _, item := range got {
			if strings.HasPrefix(item, blocked) {
				t.Fatalf("buildDesktopChildEnv() leaked %q in %#v", blocked, got)
			}
		}
	}
	for _, want := range []string{"HOME=/tmp/home", "PATH=/usr/bin"} {
		found := false
		for _, item := range got {
			if item == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("buildDesktopChildEnv() missing %q in %#v", want, got)
		}
	}
}
