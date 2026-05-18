//go:build wailsdesktop

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompareDesktopUpdateVersions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		current    string
		latest     string
		want       int
		comparable bool
	}{
		{name: "older", current: "0.2.41", latest: "0.2.42", want: -1, comparable: true},
		{name: "equal", current: "v0.2.42", latest: "0.2.42", want: 0, comparable: true},
		{name: "newer", current: "0.3.0", latest: "0.2.42", want: 1, comparable: true},
		{name: "release after prerelease", current: "1.0.0-beta.1", latest: "1.0.0", want: -1, comparable: true},
		{name: "dev", current: "dev", latest: "1.0.0", comparable: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, comparable := compareDesktopUpdateVersions(tc.current, tc.latest)
			if comparable != tc.comparable {
				t.Fatalf("comparable = %v, want %v", comparable, tc.comparable)
			}
			if comparable && got != tc.want {
				t.Fatalf("compareDesktopUpdateVersions() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCheckDesktopUpdate_ReportsAvailableWithoutDownload(t *testing.T) {
	t.Parallel()

	asset := []byte("desktop update asset")
	server := newDesktopUpdateTestServer(t, asset)

	result, err := checkDesktopUpdate(context.Background(), desktopUpdateCheckOptions{
		CurrentVersion: "0.2.41",
		ManifestURL:    server.URL + "/update.json",
	})
	if err != nil {
		t.Fatalf("checkDesktopUpdate() error = %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
	if result.Downloaded {
		t.Fatalf("Downloaded = true, want false")
	}
	if result.Status != "update_available" {
		t.Fatalf("Status = %q, want update_available", result.Status)
	}
}

func TestCheckDesktopUpdate_AutoDownloadsAndVerifiesAsset(t *testing.T) {
	t.Parallel()

	asset := []byte("desktop update asset")
	server := newDesktopUpdateTestServer(t, asset)
	cacheDir := t.TempDir()

	result, err := checkDesktopUpdate(context.Background(), desktopUpdateCheckOptions{
		AutoDownload:   true,
		CacheDir:       cacheDir,
		CurrentVersion: "0.2.41",
		ManifestURL:    server.URL + "/update.json",
	})
	if err != nil {
		t.Fatalf("checkDesktopUpdate() error = %v", err)
	}
	if !result.Downloaded {
		t.Fatalf("Downloaded = false, want true")
	}
	if result.DownloadStatus != "downloaded" {
		t.Fatalf("DownloadStatus = %q, want downloaded", result.DownloadStatus)
	}
	if result.DownloadPath == "" {
		t.Fatalf("DownloadPath is empty")
	}
	got, err := os.ReadFile(result.DownloadPath)
	if err != nil {
		t.Fatalf("ReadFile(download) error = %v", err)
	}
	if string(got) != string(asset) {
		t.Fatalf("downloaded asset = %q, want %q", string(got), string(asset))
	}
	if filepath.Dir(filepath.Dir(result.DownloadPath)) != filepath.Clean(cacheDir) {
		t.Fatalf("download path = %q, want under %q", result.DownloadPath, cacheDir)
	}
}

func TestCheckDesktopUpdate_UnknownCurrentVersionDoesNotDownload(t *testing.T) {
	t.Parallel()

	asset := []byte("desktop update asset")
	server := newDesktopUpdateTestServer(t, asset)

	result, err := checkDesktopUpdate(context.Background(), desktopUpdateCheckOptions{
		AutoDownload:   true,
		CacheDir:       t.TempDir(),
		CurrentVersion: "dev",
		ManifestURL:    server.URL + "/update.json",
	})
	if err != nil {
		t.Fatalf("checkDesktopUpdate() error = %v", err)
	}
	if result.Status != "current_version_unknown" {
		t.Fatalf("Status = %q, want current_version_unknown", result.Status)
	}
	if result.Downloaded {
		t.Fatalf("Downloaded = true, want false")
	}
}

func newDesktopUpdateTestServer(t *testing.T, asset []byte) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/update.json":
			sum := sha256.Sum256(asset)
			manifest := desktopUpdateManifest{
				Version:     "0.2.42",
				ReleaseDate: "2026-03-29T12:34:56Z",
				Platforms: map[string]desktopUpdatePlatform{
					desktopUpdatePlatformKey(runtime.GOOS, runtime.GOARCH): {
						URL:      server.URL + "/asset.tar.gz",
						Size:     int64(len(asset)),
						Checksum: "sha256:" + hex.EncodeToString(sum[:]),
					},
				},
			}
			_ = json.NewEncoder(w).Encode(manifest)
		case "/asset.tar.gz":
			_, _ = w.Write(asset)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
