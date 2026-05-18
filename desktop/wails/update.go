//go:build wailsdesktop

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultDesktopUpdateManifestURL = "https://github.com/quailyquaily/mistermorph/releases/latest/download/update.json"

var desktopVersion = "dev"

type desktopUpdateCheckOptions struct {
	AutoDownload   bool
	CacheDir       string
	CurrentVersion string
	ManifestURL    string
}

type desktopUpdateManifest struct {
	Version      string                           `json:"version"`
	ReleaseDate  string                           `json:"release_date"`
	ReleaseNotes string                           `json:"release_notes"`
	Platforms    map[string]desktopUpdatePlatform `json:"platforms"`
	Mandatory    bool                             `json:"mandatory"`
}

type desktopUpdatePlatform struct {
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
}

type DesktopUpdateCheckResult struct {
	Status          string `json:"status"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	Platform        string `json:"platform"`
	UpdateAvailable bool   `json:"update_available"`
	Mandatory       bool   `json:"mandatory"`
	ReleaseDate     string `json:"release_date,omitempty"`
	ReleaseNotes    string `json:"release_notes,omitempty"`
	AssetURL        string `json:"asset_url,omitempty"`
	AssetSize       int64  `json:"asset_size,omitempty"`
	Checksum        string `json:"checksum,omitempty"`
	Downloaded      bool   `json:"downloaded"`
	DownloadStatus  string `json:"download_status,omitempty"`
	DownloadPath    string `json:"download_path,omitempty"`
}

func runDesktopCheckUpdateCommand(ctx context.Context, cfg desktopRuntimeConfig, out io.Writer) error {
	result, err := checkDesktopUpdate(ctx, desktopUpdateCheckOptions{
		AutoDownload:   cfg.AutoUpdate.Enabled,
		CurrentVersion: desktopVersion,
	})
	if err != nil {
		return err
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func startDesktopAutoUpdateCheck(ctx context.Context, cfg desktopAutoUpdateConfig, logWriter io.Writer) {
	if !cfg.Enabled {
		return
	}

	go func() {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		result, err := checkDesktopUpdate(checkCtx, desktopUpdateCheckOptions{
			AutoDownload:   true,
			CurrentVersion: desktopVersion,
		})
		if err != nil {
			logDesktopUpdateEvent(logWriter, "auto_update_failed error=%q", compactDesktopLogValue(err.Error(), 1000))
			return
		}
		logDesktopUpdateEvent(
			logWriter,
			"auto_update_checked status=%q current=%q latest=%q platform=%q downloaded=%t",
			result.Status,
			result.CurrentVersion,
			result.LatestVersion,
			result.Platform,
			result.Downloaded,
		)
	}()
}

func logDesktopUpdateEvent(logWriter io.Writer, format string, args ...any) {
	line := "desktop_update " + fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintln(os.Stderr, line)
	if logWriter != nil {
		_, _ = fmt.Fprintln(logWriter, line)
	}
}

func checkDesktopUpdate(ctx context.Context, opts desktopUpdateCheckOptions) (DesktopUpdateCheckResult, error) {
	manifestURL := strings.TrimSpace(opts.ManifestURL)
	if manifestURL == "" {
		manifestURL = defaultDesktopUpdateManifestURL
	}

	manifest, err := fetchDesktopUpdateManifest(ctx, manifestURL)
	if err != nil {
		return DesktopUpdateCheckResult{}, err
	}

	platformKey := desktopUpdatePlatformKey(runtime.GOOS, runtime.GOARCH)
	platform, ok := manifest.Platforms[platformKey]
	if !ok {
		return DesktopUpdateCheckResult{}, fmt.Errorf("update manifest has no platform %q", platformKey)
	}
	if strings.TrimSpace(platform.URL) == "" {
		return DesktopUpdateCheckResult{}, fmt.Errorf("update manifest platform %q has empty url", platformKey)
	}
	if strings.TrimSpace(platform.Checksum) == "" {
		return DesktopUpdateCheckResult{}, fmt.Errorf("update manifest platform %q has empty checksum", platformKey)
	}

	currentVersion := normalizeDesktopUpdateVersion(opts.CurrentVersion)
	if currentVersion == "" {
		currentVersion = normalizeDesktopUpdateVersion(desktopVersion)
	}
	latestVersion := normalizeDesktopUpdateVersion(manifest.Version)
	if latestVersion == "" {
		return DesktopUpdateCheckResult{}, fmt.Errorf("update manifest has empty version")
	}

	result := DesktopUpdateCheckResult{
		Status:         "up_to_date",
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		Platform:       platformKey,
		Mandatory:      manifest.Mandatory,
		ReleaseDate:    strings.TrimSpace(manifest.ReleaseDate),
		ReleaseNotes:   manifest.ReleaseNotes,
		AssetURL:       strings.TrimSpace(platform.URL),
		AssetSize:      platform.Size,
		Checksum:       strings.TrimSpace(platform.Checksum),
	}

	compare, comparable := compareDesktopUpdateVersions(currentVersion, latestVersion)
	if !comparable {
		result.Status = "current_version_unknown"
		return result, nil
	}
	if compare >= 0 {
		return result, nil
	}

	result.Status = "update_available"
	result.UpdateAvailable = true
	if opts.AutoDownload {
		downloadPath, downloadStatus, err := downloadDesktopUpdateAsset(ctx, opts, latestVersion, platformKey, platform)
		if err != nil {
			return DesktopUpdateCheckResult{}, err
		}
		result.Status = "downloaded"
		result.Downloaded = true
		result.DownloadStatus = downloadStatus
		result.DownloadPath = downloadPath
	}
	return result, nil
}

func fetchDesktopUpdateManifest(ctx context.Context, manifestURL string) (desktopUpdateManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(manifestURL), nil)
	if err != nil {
		return desktopUpdateManifest{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", desktopBackendHTTPUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return desktopUpdateManifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return desktopUpdateManifest{}, fmt.Errorf("update manifest http status %d", resp.StatusCode)
	}

	var out desktopUpdateManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return desktopUpdateManifest{}, fmt.Errorf("decode update manifest: %w", err)
	}
	return out, nil
}

func downloadDesktopUpdateAsset(ctx context.Context, opts desktopUpdateCheckOptions, version string, platformKey string, platform desktopUpdatePlatform) (string, string, error) {
	cacheDir := strings.TrimSpace(opts.CacheDir)
	if cacheDir == "" {
		var err error
		cacheDir, err = desktopUpdateCacheDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve update cache dir: %w", err)
		}
	}

	assetName := desktopUpdateAssetName(platform.URL, version, platformKey)
	dstDir := filepath.Join(cacheDir, sanitizeTag(version))
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create update cache dir: %w", err)
	}
	dstPath := filepath.Join(dstDir, assetName)

	if _, err := os.Stat(dstPath); err == nil {
		if err := verifyDesktopUpdateChecksum(dstPath, platform.Checksum); err == nil {
			return dstPath, "cached", nil
		}
	}

	if err := downloadFile(ctx, dstPath, platform.URL); err != nil {
		return "", "", fmt.Errorf("download update asset: %w", err)
	}
	if err := verifyDesktopUpdateChecksum(dstPath, platform.Checksum); err != nil {
		_ = os.Remove(dstPath)
		return "", "", err
	}
	return dstPath, "downloaded", nil
}

func desktopUpdateCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mistermorph", "desktop", "updates"), nil
}

func verifyDesktopUpdateChecksum(path string, checksum string) error {
	want, err := parseDesktopSHA256Checksum(checksum)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := h.Sum(nil)
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		return fmt.Errorf("update asset checksum mismatch")
	}
	return nil
}

func parseDesktopSHA256Checksum(checksum string) ([]byte, error) {
	checksum = strings.ToLower(strings.TrimSpace(checksum))
	checksum = strings.TrimPrefix(checksum, "sha256:")
	if checksum == "" {
		return nil, fmt.Errorf("empty update checksum")
	}
	raw, err := hex.DecodeString(checksum)
	if err != nil {
		return nil, fmt.Errorf("decode update checksum: %w", err)
	}
	if len(raw) != sha256.Size {
		return nil, fmt.Errorf("invalid update checksum length %d", len(raw))
	}
	return raw, nil
}

func desktopUpdateAssetName(rawURL string, version string, platformKey string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err == nil {
		if base := strings.TrimSpace(filepath.Base(parsed.Path)); base != "" && base != "." && base != "/" {
			return sanitizeDesktopUpdateFilename(base)
		}
	}
	return sanitizeDesktopUpdateFilename("mistermorph-desktop-" + platformKey + "-" + sanitizeTag(version))
}

func sanitizeDesktopUpdateFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "" || name == "." {
		return "update-asset"
	}
	return name
}

func desktopUpdatePlatformKey(goos string, goarch string) string {
	goos = strings.TrimSpace(goos)
	goarch = strings.TrimSpace(goarch)
	if goos == "darwin" {
		return "macos-" + goarch
	}
	return goos + "-" + goarch
}

func normalizeDesktopUpdateVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	return version
}

func compareDesktopUpdateVersions(current string, latest string) (int, bool) {
	currentParsed, ok := parseDesktopUpdateVersion(current)
	if !ok {
		return 0, false
	}
	latestParsed, ok := parseDesktopUpdateVersion(latest)
	if !ok {
		return 0, false
	}
	return compareParsedDesktopUpdateVersions(currentParsed, latestParsed), true
}

type parsedDesktopUpdateVersion struct {
	parts []int
	pre   string
}

func parseDesktopUpdateVersion(version string) (parsedDesktopUpdateVersion, bool) {
	version = normalizeDesktopUpdateVersion(version)
	if version == "" || strings.EqualFold(version, "dev") {
		return parsedDesktopUpdateVersion{}, false
	}
	if before, _, ok := strings.Cut(version, "+"); ok {
		version = before
	}
	core, pre, _ := strings.Cut(version, "-")
	segments := strings.Split(core, ".")
	parts := make([]int, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			return parsedDesktopUpdateVersion{}, false
		}
		part, err := strconv.Atoi(segment)
		if err != nil || part < 0 {
			return parsedDesktopUpdateVersion{}, false
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return parsedDesktopUpdateVersion{}, false
	}
	return parsedDesktopUpdateVersion{parts: parts, pre: pre}, true
}

func compareParsedDesktopUpdateVersions(a parsedDesktopUpdateVersion, b parsedDesktopUpdateVersion) int {
	maxLen := len(a.parts)
	if len(b.parts) > maxLen {
		maxLen = len(b.parts)
	}
	for i := 0; i < maxLen; i++ {
		av := 0
		if i < len(a.parts) {
			av = a.parts[i]
		}
		bv := 0
		if i < len(b.parts) {
			bv = b.parts[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	if a.pre == b.pre {
		return 0
	}
	if a.pre == "" {
		return 1
	}
	if b.pre == "" {
		return -1
	}
	if a.pre < b.pre {
		return -1
	}
	return 1
}
