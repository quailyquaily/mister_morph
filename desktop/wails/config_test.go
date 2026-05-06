//go:build wailsdesktop

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDesktopConfigPath_ExplicitFlagWins(t *testing.T) {
	home := t.TempDir()
	setDesktopTestHome(t, home)

	got, explicit := resolveDesktopConfigPath([]string{"--config", "~/custom.yaml"})
	want := filepath.Join(home, "custom.yaml")
	if got != filepath.Clean(want) {
		t.Fatalf("resolveDesktopConfigPath() path = %q, want %q", got, filepath.Clean(want))
	}
	if !explicit {
		t.Fatalf("resolveDesktopConfigPath() explicit = false, want true")
	}
}

func TestResolveDesktopConfigPath_DefaultIgnoresCWD(t *testing.T) {
	home := t.TempDir()
	setDesktopTestHome(t, home)

	wd := t.TempDir()
	restoreDesktopWD(t, wd)
	if err := os.WriteFile("config.yaml", []byte("llm:\n  provider: openai\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config.yaml) error = %v", err)
	}

	got, explicit := resolveDesktopConfigPath(nil)
	if got != "" {
		t.Fatalf("resolveDesktopConfigPath() path = %q, want empty", got)
	}
	if explicit {
		t.Fatalf("resolveDesktopConfigPath() explicit = true, want false")
	}
}

func restoreDesktopWD(t *testing.T, wd string) {
	t.Helper()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("Chdir(%q) error = %v", wd, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})
}

func setDesktopTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}
