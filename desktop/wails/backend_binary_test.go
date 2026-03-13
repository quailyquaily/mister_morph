//go:build wailsdesktop

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopBackendAutoDownloadEnabled(t *testing.T) {
	t.Setenv(desktopBackendAutoDownloadEnv, "")
	if !desktopBackendAutoDownloadEnabled() {
		t.Fatalf("expected default auto-download to be enabled")
	}

	t.Setenv(desktopBackendAutoDownloadEnv, "false")
	if desktopBackendAutoDownloadEnabled() {
		t.Fatalf("expected auto-download to be disabled when env=false")
	}

	t.Setenv(desktopBackendAutoDownloadEnv, "not-a-bool")
	if !desktopBackendAutoDownloadEnabled() {
		t.Fatalf("expected invalid bool env to fallback to enabled")
	}
}

func TestPickReleaseAsset(t *testing.T) {
	assets := []githubReleaseAsset{
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		{Name: "mistermorph_0.2.1_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/mistermorph_0.2.1_linux_amd64.tar.gz"},
		{Name: "mistermorph_0.2.1_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/mistermorph_0.2.1_darwin_arm64.tar.gz"},
	}
	asset, err := pickReleaseAsset(assets, "linux", "amd64")
	if err != nil {
		t.Fatalf("pickReleaseAsset() error = %v", err)
	}
	if asset.Name != "mistermorph_0.2.1_linux_amd64.tar.gz" {
		t.Fatalf("unexpected asset: %q", asset.Name)
	}
}

func TestResolveDesktopBackendCandidates(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "mistermorph-desktop")
	explicit := filepath.Join(t.TempDir(), "mistermorph")
	t.Setenv(desktopBackendBinEnv, "")

	candidates := resolveDesktopBackendCandidates(exePath, explicit)
	if len(candidates) == 0 {
		t.Fatalf("expected non-empty candidates")
	}
	if candidates[0] != filepath.Clean(explicit) {
		t.Fatalf("first candidate = %q, want %q", candidates[0], filepath.Clean(explicit))
	}
}

func TestResolveDesktopBackendCandidates_EnvBinary(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "mistermorph-custom")
	t.Setenv(desktopBackendBinEnv, envPath)
	candidates := resolveDesktopBackendCandidates("", "")
	found := false
	for _, c := range candidates {
		if c == filepath.Clean(envPath) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected env candidate %q in list: %#v", envPath, candidates)
	}
}

func TestIsExecutableFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "mistermorph")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if !isExecutableFile(file) {
		t.Fatalf("expected file to be executable")
	}
}
