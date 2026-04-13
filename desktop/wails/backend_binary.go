//go:build wailsdesktop

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goRuntime "runtime"
	"strconv"
	"strings"
)

const (
	desktopBackendBinEnv          = "MISTERMORPH_DESKTOP_BACKEND_BIN"
	desktopBackendAutoDownloadEnv = "MISTERMORPH_DESKTOP_BACKEND_AUTO_DOWNLOAD"
	desktopBackendVersionEnv      = "MISTERMORPH_DESKTOP_BACKEND_VERSION"
	desktopBackendCacheDirEnv     = "MISTERMORPH_DESKTOP_BACKEND_CACHE_DIR"
	desktopAppDirEnv              = "APPDIR"

	desktopBackendRepoReleaseAPI = "https://api.github.com/repos/quailyquaily/mistermorph/releases"
	desktopBackendHTTPUserAgent  = "mistermorph-desktop-host"
)

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func resolveDesktopBackendCandidates(selfExePath string, explicitPath string) []string {
	out := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(raw string) {
		clean := normalizeDesktopPathCandidate(raw)
		if clean == "" {
			return
		}
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}

	add(explicitPath)
	add(os.Getenv(desktopBackendBinEnv))

	if appDir := strings.TrimSpace(os.Getenv(desktopAppDirEnv)); appDir != "" {
		addDesktopBackendCandidates(add, filepath.Join(appDir, "usr", "bin"))
		addDesktopBackendCandidates(add, appDir)
	}
	if strings.TrimSpace(selfExePath) != "" {
		exeDir := filepath.Dir(filepath.Clean(selfExePath))
		addDesktopBackendCandidates(add, exeDir)
		if shouldSearchDesktopExecutableChildBin(exeDir) {
			addDesktopBackendCandidates(add, filepath.Join(exeDir, "bin"))
		}
	}
	if wd, err := os.Getwd(); err == nil {
		addDesktopBackendCandidates(add, filepath.Join(wd, "bin"))
	}

	if path, err := exec.LookPath(desktopBackendBinaryBaseName()); err == nil {
		add(path)
	}
	return out
}

func addDesktopBackendCandidates(add func(string), dir string) {
	for _, name := range desktopBackendCandidateBaseNames() {
		add(filepath.Join(dir, name))
	}
}

func normalizeDesktopPathCandidate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	clean := filepath.Clean(raw)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved)
	}

	ancestor, suffix := splitExistingPathPrefix(clean)
	if ancestor == "" {
		return clean
	}
	resolvedAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return clean
	}
	if suffix == "" || suffix == "." {
		return filepath.Clean(resolvedAncestor)
	}
	return filepath.Clean(filepath.Join(resolvedAncestor, suffix))
}

func splitExistingPathPrefix(path string) (string, string) {
	clean := filepath.Clean(path)
	suffix := ""
	for {
		if _, err := os.Lstat(clean); err == nil {
			return clean, suffix
		}
		next := filepath.Dir(clean)
		if next == clean {
			return "", ""
		}
		base := filepath.Base(clean)
		if suffix == "" {
			suffix = base
		} else {
			suffix = filepath.Join(base, suffix)
		}
		clean = next
	}
}

func shouldSearchDesktopExecutableChildBin(exeDir string) bool {
	switch strings.ToLower(strings.TrimSpace(filepath.Base(exeDir))) {
	case "bin", "macos":
		return false
	default:
		return true
	}
}

func isExecutableFile(path string) bool {
	path = normalizeDesktopPathCandidate(path)
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	if goRuntime.GOOS == "windows" {
		return true
	}
	return fi.Mode()&0o111 != 0
}

func desktopBackendBinaryBaseName() string {
	if goRuntime.GOOS == "windows" {
		return "mistermorph.exe"
	}
	return "mistermorph"
}

func desktopLegacyBundledBackendBinaryBaseName() string {
	if goRuntime.GOOS == "windows" {
		return "mistermorph-backend.exe"
	}
	return "mistermorph-backend"
}

func desktopBackendCandidateBaseNames() []string {
	base := desktopBackendBinaryBaseName()
	legacy := desktopLegacyBundledBackendBinaryBaseName()
	if legacy == base {
		return []string{base}
	}
	return []string{base, legacy}
}

func desktopBackendAutoDownloadEnabled() bool {
	raw := strings.TrimSpace(os.Getenv(desktopBackendAutoDownloadEnv))
	if raw == "" {
		return true
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return v
}

func desktopBackendVersion() string {
	v := strings.TrimSpace(os.Getenv(desktopBackendVersionEnv))
	if v == "" {
		return "latest"
	}
	return v
}

func desktopBackendCacheDir() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv(desktopBackendCacheDirEnv)); explicit != "" {
		return filepath.Clean(explicit), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mistermorph", "desktop", "backend"), nil
}

func downloadMistermorphBinary(ctx context.Context, version string) (string, error) {
	rel, err := fetchGitHubRelease(ctx, version)
	if err != nil {
		return "", err
	}
	asset, err := pickReleaseAsset(rel.Assets, goRuntime.GOOS, goRuntime.GOARCH)
	if err != nil {
		return "", err
	}

	cacheDir, err := desktopBackendCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	tag := strings.TrimSpace(rel.TagName)
	if tag == "" {
		tag = "latest"
	}
	dstName := fmt.Sprintf("mistermorph-%s-%s-%s", sanitizeTag(tag), goRuntime.GOOS, goRuntime.GOARCH)
	if goRuntime.GOOS == "windows" {
		dstName += ".exe"
	}
	dstPath := filepath.Join(cacheDir, dstName)
	if isExecutableFile(dstPath) {
		return dstPath, nil
	}

	archivePath := filepath.Join(cacheDir, asset.Name)
	if err := downloadFile(ctx, archivePath, asset.BrowserDownloadURL); err != nil {
		return "", fmt.Errorf("download asset %s: %w", asset.Name, err)
	}
	if err := extractBinaryFromArchive(archivePath, dstPath, desktopBackendBinaryBaseName()); err != nil {
		return "", err
	}
	if goRuntime.GOOS != "windows" {
		_ = os.Chmod(dstPath, 0o755)
	}
	return dstPath, nil
}

func fetchGitHubRelease(ctx context.Context, version string) (githubRelease, error) {
	endpoint := desktopBackendRepoReleaseAPI + "/latest"
	if v := strings.TrimSpace(version); v != "" && !strings.EqualFold(v, "latest") {
		endpoint = desktopBackendRepoReleaseAPI + "/tags/" + url.PathEscape(v)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", desktopBackendHTTPUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return githubRelease{}, fmt.Errorf("github release api status %d", resp.StatusCode)
	}

	var out githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return githubRelease{}, fmt.Errorf("decode release metadata: %w", err)
	}
	return out, nil
}

func pickReleaseAsset(assets []githubReleaseAsset, goos, goarch string) (githubReleaseAsset, error) {
	suffix := "_" + goos + "_" + goarch
	for _, asset := range assets {
		name := strings.TrimSpace(asset.Name)
		if name == "" || strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			continue
		}
		if !strings.Contains(name, suffix) {
			continue
		}
		if strings.Contains(strings.ToLower(name), "checksum") {
			continue
		}
		if strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip") {
			return asset, nil
		}
	}
	return githubReleaseAsset{}, fmt.Errorf("no release asset found for %s/%s", goos, goarch)
}

func downloadFile(ctx context.Context, dstPath string, srcURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(srcURL), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", desktopBackendHTTPUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}

	tmpPath := dstPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
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

func extractBinaryFromArchive(archivePath string, dstPath string, binaryName string) error {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractBinaryFromZip(archivePath, dstPath, binaryName)
	}
	if strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") {
		return extractBinaryFromTarGz(archivePath, dstPath, binaryName)
	}
	return fmt.Errorf("unsupported archive format: %s", archivePath)
}

func extractBinaryFromZip(archivePath string, dstPath string, binaryName string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if filepath.Base(f.Name) != binaryName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeExtractedBinary(dstPath, rc)
	}
	return fmt.Errorf("binary %s not found in zip", binaryName)
}

func extractBinaryFromTarGz(archivePath string, dstPath string, binaryName string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr == nil || hdr.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(hdr.Name) != binaryName {
			continue
		}
		return writeExtractedBinary(dstPath, tr)
	}
	return fmt.Errorf("binary %s not found in tar.gz", binaryName)
}

func writeExtractedBinary(dstPath string, r io.Reader) error {
	tmpPath := dstPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
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

func sanitizeTag(tag string) string {
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "v"))
	tag = strings.ReplaceAll(tag, " ", "_")
	tag = strings.ReplaceAll(tag, "/", "_")
	if tag == "" {
		return "latest"
	}
	return tag
}
