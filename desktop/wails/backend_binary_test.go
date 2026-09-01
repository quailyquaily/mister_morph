//go:build wailsdesktop

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDesktopBackendBinaryName(t *testing.T) {
	want := "morph"
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got := desktopBackendBinaryBaseName(); got != want {
		t.Fatalf("desktopBackendBinaryBaseName() = %q, want %q", got, want)
	}
	if got := desktopBackendCandidateBaseNames()[0]; got != want {
		t.Fatalf("first backend candidate = %q, want %q", got, want)
	}
}

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
	for _, name := range desktopBackendCandidateBaseNames() {
		appIdx := candidateIndex(candidates, filepath.Join(appDir, "usr", "bin", name))
		wdIdx := candidateIndex(candidates, filepath.Join(wd, "bin", name))
		if appIdx == -1 || wdIdx == -1 || appIdx >= wdIdx {
			t.Fatalf("candidate %q must prefer APPDIR over working directory in %#v", name, candidates)
		}
	}
	unexpected := filepath.Join(appDir, "usr", "bin", "bin", desktopBackendBinaryBaseName())
	for _, c := range candidates {
		if sameCleanPath(c, unexpected) {
			t.Fatalf("unexpected nested bin candidate %q in %#v", c, candidates)
		}
	}
}

func TestResolveDesktopBackendCandidates_SiblingBundledBackendBeforeCLIAndLegacyNames(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "MisterMorph.app", "Contents", "MacOS", "MrMorph")
	t.Setenv(desktopBackendBinEnv, "")

	candidates := resolveDesktopBackendCandidates(exePath, "")
	previous := -1
	for _, name := range desktopBackendCandidateBaseNames() {
		idx := candidateIndex(candidates, filepath.Join(filepath.Dir(exePath), name))
		if idx == -1 || idx <= previous {
			t.Fatalf("sibling candidate %q has invalid order in %#v", name, candidates)
		}
		previous = idx
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

func candidateIndex(candidates []string, target string) int {
	for i, candidate := range candidates {
		if sameCleanPath(candidate, target) {
			return i
		}
	}
	return -1
}
