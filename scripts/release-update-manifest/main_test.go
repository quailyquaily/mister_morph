package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"v0.2.41":      "0.2.41",
		"  v1.2.3  ":   "1.2.3",
		"1.2.3-beta.1": "1.2.3-beta.1",
		"":             "",
	}

	for input, want := range cases {
		if got := normalizeVersion(input); got != want {
			t.Fatalf("normalizeVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPlatformKeyForAssetName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		asset   string
		want    string
		matched bool
	}{
		{name: "linux", asset: "mistermorph-desktop-linux-amd64.AppImage", want: "linux-amd64", matched: true},
		{name: "linux versioned", asset: "mistermorph-desktop-v0.2.93-linux-amd64.AppImage", want: "linux-amd64", matched: true},
		{name: "linux tarball", asset: "mistermorph-desktop-linux-amd64.tar.gz", want: "linux-amd64", matched: true},
		{name: "linux deb", asset: "mistermorph-desktop-linux-amd64.deb", matched: false},
		{name: "macos", asset: "mistermorph-desktop-darwin-arm64.dmg", want: "macos-arm64", matched: true},
		{name: "macos versioned", asset: "mistermorph-desktop-v0.2.93-darwin-arm64.dmg", want: "macos-arm64", matched: true},
		{name: "macos tarball", asset: "mistermorph-desktop-darwin-arm64.tar.gz", want: "macos-arm64", matched: true},
		{name: "windows", asset: "mistermorph-desktop-windows-amd64.zip", want: "windows-amd64", matched: true},
		{name: "windows versioned", asset: "mistermorph-desktop-v0.2.93-windows-amd64.zip", want: "windows-amd64", matched: true},
		{name: "cli asset", asset: "mistermorph_0.2.41_linux_amd64.tar.gz", matched: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := platformKeyForAssetName(tc.asset)
			if ok != tc.matched {
				t.Fatalf("platformKeyForAssetName(%q) matched = %v, want %v", tc.asset, ok, tc.matched)
			}
			if got != tc.want {
				t.Fatalf("platformKeyForAssetName(%q) = %q, want %q", tc.asset, got, tc.want)
			}
		})
	}
}

func TestLoadChecksums(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	linuxDir := filepath.Join(root, "linux-amd64")
	if err := os.MkdirAll(linuxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	checksumPath := filepath.Join(linuxDir, "mistermorph-desktop-linux-amd64.AppImage.sha256")
	if err := os.WriteFile(checksumPath, []byte("ABCDEF0123 *mistermorph-desktop-linux-amd64.AppImage\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := loadChecksums(root)
	if err != nil {
		t.Fatalf("loadChecksums() error = %v", err)
	}

	want := "sha256:abcdef0123"
	if got["mistermorph-desktop-linux-amd64.AppImage"] != want {
		t.Fatalf("checksum = %q, want %q", got["mistermorph-desktop-linux-amd64.AppImage"], want)
	}
}

func TestBuildUpdateManifest(t *testing.T) {
	t.Parallel()

	release := releaseMetadata{
		TagName:     "v0.2.41",
		Body:        "## Changes\n\n- Added update manifest",
		PublishedAt: "2026-03-29T12:34:56Z",
		Assets: []releaseAsset{
			{
				Name:               "mistermorph-desktop-darwin-arm64.dmg",
				BrowserDownloadURL: "https://github.com/quailyquaily/mistermorph/releases/download/v0.2.41/mistermorph-desktop-darwin-arm64.dmg",
				Size:               123,
			},
			{
				Name:               "mistermorph-desktop-darwin-arm64.tar.gz",
				BrowserDownloadURL: "https://github.com/quailyquaily/mistermorph/releases/download/v0.2.41/mistermorph-desktop-darwin-arm64.tar.gz",
				Size:               124,
			},
			{
				Name:               "mistermorph-desktop-linux-amd64.AppImage",
				BrowserDownloadURL: "https://github.com/quailyquaily/mistermorph/releases/download/v0.2.41/mistermorph-desktop-linux-amd64.AppImage",
				Size:               456,
			},
			{
				Name:               "mistermorph-desktop-linux-amd64.tar.gz",
				BrowserDownloadURL: "https://github.com/quailyquaily/mistermorph/releases/download/v0.2.41/mistermorph-desktop-linux-amd64.tar.gz",
				Size:               457,
			},
			{
				Name:               "mistermorph-desktop-windows-amd64.zip",
				BrowserDownloadURL: "https://github.com/quailyquaily/mistermorph/releases/download/v0.2.41/mistermorph-desktop-windows-amd64.zip",
				Size:               789,
			},
			{
				Name:               "mistermorph_0.2.41_linux_amd64.tar.gz",
				BrowserDownloadURL: "https://example.com/ignored",
				Size:               999,
			},
		},
	}

	checksums := map[string]string{
		"mistermorph-desktop-darwin-arm64.dmg":     "sha256:aaa",
		"mistermorph-desktop-darwin-arm64.tar.gz":  "sha256:aaat",
		"mistermorph-desktop-linux-amd64.AppImage": "sha256:bbb",
		"mistermorph-desktop-linux-amd64.tar.gz":   "sha256:bbbt",
		"mistermorph-desktop-windows-amd64.zip":    "sha256:ccc",
		"mistermorph_0.2.41_linux_amd64.tar.gz":    "sha256:ignored",
	}

	got, err := buildUpdateManifest(release, checksums)
	if err != nil {
		t.Fatalf("buildUpdateManifest() error = %v", err)
	}

	if got.Version != "0.2.41" {
		t.Fatalf("Version = %q, want %q", got.Version, "0.2.41")
	}
	if got.ReleaseDate != "2026-03-29T12:34:56Z" {
		t.Fatalf("ReleaseDate = %q, want %q", got.ReleaseDate, "2026-03-29T12:34:56Z")
	}
	if got.ReleaseNotes != release.Body {
		t.Fatalf("ReleaseNotes = %q, want %q", got.ReleaseNotes, release.Body)
	}
	if got.Mandatory {
		t.Fatalf("Mandatory = true, want false")
	}

	if platform := got.Platforms["macos-arm64"]; platform.URL != "https://github.com/quailyquaily/mistermorph/releases/download/v0.2.41/mistermorph-desktop-darwin-arm64.tar.gz" || platform.Checksum != "sha256:aaat" || platform.Size != 124 {
		t.Fatalf("macos-arm64 platform = %#v", platform)
	}
	if platform := got.Platforms["linux-amd64"]; platform.URL != "https://github.com/quailyquaily/mistermorph/releases/download/v0.2.41/mistermorph-desktop-linux-amd64.tar.gz" || platform.Checksum != "sha256:bbbt" || platform.Size != 457 {
		t.Fatalf("linux-amd64 platform = %#v", platform)
	}
	if platform := got.Platforms["windows-amd64"]; platform.URL == "" || platform.Checksum != "sha256:ccc" || platform.Size != 789 {
		t.Fatalf("windows-amd64 platform = %#v", platform)
	}
	if _, ok := got.Platforms["darwin-arm64"]; ok {
		t.Fatalf("unexpected darwin-arm64 platform entry")
	}
}

func TestRunWritesManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	releaseJSONPath := filepath.Join(root, "release.json")
	artifactsDir := filepath.Join(root, "artifacts")
	outputPath := filepath.Join(root, "dist", "update.json")

	if err := os.MkdirAll(filepath.Join(artifactsDir, "linux-amd64"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(artifactsDir, "linux-amd64", "mistermorph-desktop-linux-amd64.AppImage.sha256"),
		[]byte("0123456789abcdef *mistermorph-desktop-linux-amd64.AppImage\n"),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	release := releaseMetadata{
		TagName:     "v0.2.41",
		Body:        "notes",
		PublishedAt: "2026-03-29T12:34:56Z",
		Assets: []releaseAsset{
			{
				Name:               "mistermorph-desktop-linux-amd64.AppImage",
				BrowserDownloadURL: "https://example.com/mistermorph-desktop-linux-amd64.AppImage",
				Size:               456,
			},
		},
	}
	raw, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(releaseJSONPath, raw, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err = run([]string{
		"-release-json", releaseJSONPath,
		"-artifacts-dir", artifactsDir,
		"-output", outputPath,
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	out, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var manifest updateManifest
	if err := json.Unmarshal(out, &manifest); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if manifest.Version != "0.2.41" {
		t.Fatalf("manifest version = %q, want %q", manifest.Version, "0.2.41")
	}
	if manifest.Platforms["linux-amd64"].Checksum != "sha256:0123456789abcdef" {
		t.Fatalf("checksum = %q, want %q", manifest.Platforms["linux-amd64"].Checksum, "sha256:0123456789abcdef")
	}
}
