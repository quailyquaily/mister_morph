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
	if got, want := candidates[0], filepath.Clean(explicit); !sameCleanPath(got, want) {
		t.Fatalf("first candidate = %q, want path-equivalent to %q", got, want)
	}
}

func TestResolveDesktopBackendCandidates_EnvBinary(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "mistermorph-custom")
	t.Setenv(desktopBackendBinEnv, envPath)
	candidates := resolveDesktopBackendCandidates("", "")
	found := false
	for _, c := range candidates {
		if sameCleanPath(c, envPath) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected env candidate path-equivalent to %q in list: %#v", envPath, candidates)
	}
}

func TestResolveDesktopBackendCandidates_AppDirPreferredOverWorkingDir(t *testing.T) {
	appDir := t.TempDir()
	wd := t.TempDir()
	t.Setenv(desktopBackendBinEnv, "")
	t.Setenv(desktopAppDirEnv, appDir)

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	candidates := resolveDesktopBackendCandidates(filepath.Join(appDir, "usr", "bin", "mistermorph-desktop"), "")
	if len(candidates) < 3 {
		t.Fatalf("expected multiple candidates, got %#v", candidates)
	}
	if got, want := candidates[0], filepath.Join(appDir, "usr", "bin", desktopBackendBinaryBaseName()); !sameCleanPath(got, want) {
		t.Fatalf("first candidate = %q, want %q", got, want)
	}
	appDirCandidate := filepath.Join(appDir, "usr", "bin", desktopBackendBinaryBaseName())
	legacyCandidate := filepath.Join(appDir, "usr", "bin", desktopLegacyBundledBackendBinaryBaseName())
	wdCandidate := filepath.Join(wd, "bin", desktopBackendBinaryBaseName())
	appIdx := -1
	legacyIdx := -1
	wdIdx := -1
	for i, c := range candidates {
		if sameCleanPath(c, appDirCandidate) {
			appIdx = i
		}
		if sameCleanPath(c, legacyCandidate) {
			legacyIdx = i
		}
		if sameCleanPath(c, wdCandidate) {
			wdIdx = i
		}
	}
	if appIdx == -1 || legacyIdx == -1 || wdIdx == -1 {
		t.Fatalf("expected appdir, legacy and wd candidates in %#v", candidates)
	}
	if appIdx >= legacyIdx || legacyIdx >= wdIdx {
		t.Fatalf("candidate order appdir=%d legacy=%d wd=%d, want appdir < legacy < wd in %#v", appIdx, legacyIdx, wdIdx, candidates)
	}
	unexpected := filepath.Join(appDir, "usr", "bin", "bin", desktopBackendBinaryBaseName())
	for _, c := range candidates {
		if sameCleanPath(c, unexpected) {
			t.Fatalf("unexpected nested bin candidate %q in %#v", c, candidates)
		}
	}
}

func TestResolveDesktopBackendCandidates_SiblingBackendBeforeLegacyName(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "mistermorph-desktop.app", "Contents", "MacOS", "mistermorph-desktop")
	t.Setenv(desktopBackendBinEnv, "")

	candidates := resolveDesktopBackendCandidates(exePath, "")
	defaultCandidate := filepath.Join(filepath.Dir(exePath), desktopBackendBinaryBaseName())
	legacyCandidate := filepath.Join(filepath.Dir(exePath), desktopLegacyBundledBackendBinaryBaseName())
	defaultIdx := -1
	legacyIdx := -1
	for i, c := range candidates {
		if sameCleanPath(c, defaultCandidate) {
			defaultIdx = i
		}
		if sameCleanPath(c, legacyCandidate) {
			legacyIdx = i
		}
	}
	if defaultIdx == -1 || legacyIdx == -1 {
		t.Fatalf("expected sibling default and legacy candidates in %#v", candidates)
	}
	if defaultIdx >= legacyIdx {
		t.Fatalf("default candidate index = %d, legacy candidate index = %d, want default before legacy in %#v", defaultIdx, legacyIdx, candidates)
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

func sameCleanPath(a, b string) bool {
	a = normalizeDesktopPathCandidate(a)
	b = normalizeDesktopPathCandidate(b)
	return a == b
}
