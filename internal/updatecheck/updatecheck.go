package updatecheck

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
)

const (
	DefaultManifestURL = "https://github.com/quailyquaily/mistermorph/releases/latest/download/update.json"
	defaultUserAgent   = "mistermorph-update-check"
)

type Options struct {
	AutoDownload   bool
	CacheDir       string
	CurrentVersion string
	ManifestURL    string
	UserAgent      string
	GOOS           string
	GOARCH         string
}

type Manifest struct {
	Version      string              `json:"version"`
	ReleaseDate  string              `json:"release_date"`
	ReleaseNotes string              `json:"release_notes"`
	Platforms    map[string]Platform `json:"platforms"`
	Mandatory    bool                `json:"mandatory"`
}

type Platform struct {
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
}

type Result struct {
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

func Check(ctx context.Context, opts Options) (Result, error) {
	manifestURL := strings.TrimSpace(opts.ManifestURL)
	if manifestURL == "" {
		manifestURL = DefaultManifestURL
	}

	manifest, err := FetchManifest(ctx, manifestURL, opts.UserAgent)
	if err != nil {
		return Result{}, err
	}

	goos := strings.TrimSpace(opts.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := strings.TrimSpace(opts.GOARCH)
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	platformKey := PlatformKey(goos, goarch)
	platform, ok := manifest.Platforms[platformKey]
	if !ok {
		return Result{}, fmt.Errorf("update manifest has no platform %q", platformKey)
	}
	if strings.TrimSpace(platform.URL) == "" {
		return Result{}, fmt.Errorf("update manifest platform %q has empty url", platformKey)
	}
	if strings.TrimSpace(platform.Checksum) == "" {
		return Result{}, fmt.Errorf("update manifest platform %q has empty checksum", platformKey)
	}

	currentVersion := NormalizeVersion(opts.CurrentVersion)
	latestVersion := NormalizeVersion(manifest.Version)
	if latestVersion == "" {
		return Result{}, fmt.Errorf("update manifest has empty version")
	}

	result := Result{
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

	compare, comparable := CompareVersions(currentVersion, latestVersion)
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
		downloadPath, downloadStatus, err := DownloadAsset(ctx, opts, latestVersion, platformKey, platform)
		if err != nil {
			return Result{}, err
		}
		result.Status = "downloaded"
		result.Downloaded = true
		result.DownloadStatus = downloadStatus
		result.DownloadPath = downloadPath
	}
	return result, nil
}

func FetchManifest(ctx context.Context, manifestURL string, userAgent string) (Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(manifestURL), nil)
	if err != nil {
		return Manifest{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", normalizedUserAgent(userAgent))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Manifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Manifest{}, fmt.Errorf("update manifest http status %d", resp.StatusCode)
	}

	var out Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return Manifest{}, fmt.Errorf("decode update manifest: %w", err)
	}
	return out, nil
}

func DownloadAsset(ctx context.Context, opts Options, version string, platformKey string, platform Platform) (string, string, error) {
	cacheDir := strings.TrimSpace(opts.CacheDir)
	if cacheDir == "" {
		var err error
		cacheDir, err = CacheDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve update cache dir: %w", err)
		}
	}

	assetName := AssetName(platform.URL, version, platformKey)
	dstDir := filepath.Join(cacheDir, sanitizeTag(version))
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create update cache dir: %w", err)
	}
	dstPath := filepath.Join(dstDir, assetName)

	if _, err := os.Stat(dstPath); err == nil {
		if err := VerifyChecksum(dstPath, platform.Checksum); err == nil {
			return dstPath, "cached", nil
		}
	}

	if err := downloadFile(ctx, dstPath, platform.URL, opts.UserAgent); err != nil {
		return "", "", fmt.Errorf("download update asset: %w", err)
	}
	if err := VerifyChecksum(dstPath, platform.Checksum); err != nil {
		_ = os.Remove(dstPath)
		return "", "", err
	}
	return dstPath, "downloaded", nil
}

func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mistermorph", "updates"), nil
}

func VerifyChecksum(path string, checksum string) error {
	want, err := ParseSHA256Checksum(checksum)
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

func ParseSHA256Checksum(checksum string) ([]byte, error) {
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

func AssetName(rawURL string, version string, platformKey string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err == nil {
		if base := strings.TrimSpace(filepath.Base(parsed.Path)); base != "" && base != "." && base != "/" {
			return sanitizeFilename(base)
		}
	}
	return sanitizeFilename("mistermorph-desktop-" + platformKey + "-" + sanitizeTag(version))
}

func PlatformKey(goos string, goarch string) string {
	goos = strings.TrimSpace(goos)
	goarch = strings.TrimSpace(goarch)
	if goos == "darwin" {
		return "macos-" + goarch
	}
	return goos + "-" + goarch
}

func NormalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	return version
}

func CompareVersions(current string, latest string) (int, bool) {
	currentParsed, ok := parseVersion(current)
	if !ok {
		return 0, false
	}
	latestParsed, ok := parseVersion(latest)
	if !ok {
		return 0, false
	}
	return compareParsedVersions(currentParsed, latestParsed), true
}

type parsedVersion struct {
	parts []int
	pre   string
}

func parseVersion(version string) (parsedVersion, bool) {
	version = NormalizeVersion(version)
	if version == "" || strings.EqualFold(version, "dev") {
		return parsedVersion{}, false
	}
	if before, _, ok := strings.Cut(version, "+"); ok {
		version = before
	}
	core, pre, _ := strings.Cut(version, "-")
	segments := strings.Split(core, ".")
	parts := make([]int, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			return parsedVersion{}, false
		}
		part, err := strconv.Atoi(segment)
		if err != nil || part < 0 {
			return parsedVersion{}, false
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return parsedVersion{}, false
	}
	return parsedVersion{parts: parts, pre: pre}, true
}

func compareParsedVersions(a parsedVersion, b parsedVersion) int {
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

func downloadFile(ctx context.Context, dstPath string, rawURL string, userAgent string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", normalizedUserAgent(userAgent))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}

	tmpPath := dstPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dstPath)
}

func normalizedUserAgent(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return defaultUserAgent
	}
	return userAgent
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "" || name == "." {
		return "update-asset"
	}
	return name
}

func sanitizeTag(tag string) string {
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "v"))
	tag = strings.ReplaceAll(tag, " ", "_")
	tag = strings.ReplaceAll(tag, "/", "_")
	if tag == "" {
		return "latest"
	}
	return tag
}
