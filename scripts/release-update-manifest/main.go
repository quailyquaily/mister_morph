package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var desktopAssetNamePattern = regexp.MustCompile(`^MrMorph-(linux|darwin|windows)-([a-z0-9]+)\.(tar\.gz|AppImage|dmg|zip)$`)

type releaseMetadata struct {
	TagName     string         `json:"tag_name"`
	Body        string         `json:"body"`
	PublishedAt string         `json:"published_at"`
	Assets      []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type updateManifest struct {
	Version      string           `json:"version"`
	ReleaseDate  string           `json:"release_date"`
	ReleaseNotes string           `json:"release_notes"`
	Platforms    orderedPlatforms `json:"platforms"`
	Mandatory    bool             `json:"mandatory"`
}

type updatePlatform struct {
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
}

type matchedReleaseAsset struct {
	PlatformKey string
	GOOS        string
	Format      string
}

type orderedPlatforms map[string]updatePlatform

func (p orderedPlatforms) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}

	keys := make([]string, 0, len(p))
	for key := range p {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out bytes.Buffer
	out.WriteByte('{')
	for idx, key := range keys {
		if idx > 0 {
			out.WriteByte(',')
		}
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		valueJSON, err := json.Marshal(p[key])
		if err != nil {
			return nil, err
		}
		out.Write(keyJSON)
		out.WriteByte(':')
		out.Write(valueJSON)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "generate update manifest: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var releaseJSONPath string
	var artifactsDir string
	var outputPath string

	fs := flag.NewFlagSet("release-update-manifest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&releaseJSONPath, "release-json", "", "Path to GitHub release JSON metadata")
	fs.StringVar(&artifactsDir, "artifacts-dir", "", "Directory containing release artifact checksum files")
	fs.StringVar(&outputPath, "output", "", "Output path for update.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(releaseJSONPath) == "" {
		return errors.New("missing -release-json")
	}
	if strings.TrimSpace(artifactsDir) == "" {
		return errors.New("missing -artifacts-dir")
	}
	if strings.TrimSpace(outputPath) == "" {
		return errors.New("missing -output")
	}

	release, err := loadReleaseMetadata(releaseJSONPath)
	if err != nil {
		return err
	}
	checksums, err := loadChecksums(artifactsDir)
	if err != nil {
		return err
	}
	manifest, err := buildUpdateManifest(release, checksums)
	if err != nil {
		return err
	}

	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	payload = append(payload, '\n')

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, payload, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func loadReleaseMetadata(path string) (releaseMetadata, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return releaseMetadata{}, fmt.Errorf("read release metadata: %w", err)
	}

	var out releaseMetadata
	if err := json.Unmarshal(raw, &out); err != nil {
		return releaseMetadata{}, fmt.Errorf("decode release metadata: %w", err)
	}
	return out, nil
}

func loadChecksums(root string) (map[string]string, error) {
	checksums := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".sha256") {
			return nil
		}

		hash, err := parseChecksumFile(path)
		if err != nil {
			return err
		}
		assetName := strings.TrimSuffix(d.Name(), ".sha256")
		checksums[assetName] = hash
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load checksums: %w", err)
	}
	return checksums, nil
}

func parseChecksumFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read checksum file %s: %w", path, err)
	}

	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return "", fmt.Errorf("checksum file %s is empty", path)
	}
	return "sha256:" + strings.ToLower(strings.TrimSpace(fields[0])), nil
}

func buildUpdateManifest(release releaseMetadata, checksums map[string]string) (updateManifest, error) {
	version := normalizeVersion(release.TagName)
	if version == "" {
		return updateManifest{}, errors.New("release metadata missing tag_name")
	}

	if strings.TrimSpace(release.PublishedAt) == "" {
		return updateManifest{}, errors.New("release metadata missing published_at")
	}
	publishedAt, err := time.Parse(time.RFC3339, release.PublishedAt)
	if err != nil {
		return updateManifest{}, fmt.Errorf("parse published_at: %w", err)
	}

	platforms := make(orderedPlatforms)
	selectedAssets := make(map[string]matchedReleaseAsset)
	for _, asset := range release.Assets {
		matched, ok := matchReleaseAsset(asset.Name)
		if !ok {
			continue
		}

		checksum, ok := checksums[asset.Name]
		if !ok {
			return updateManifest{}, fmt.Errorf("missing checksum for asset %s", asset.Name)
		}
		if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			return updateManifest{}, fmt.Errorf("release asset %s missing browser_download_url", asset.Name)
		}
		if asset.Size <= 0 {
			return updateManifest{}, fmt.Errorf("release asset %s has invalid size %d", asset.Name, asset.Size)
		}

		current, exists := selectedAssets[matched.PlatformKey]
		if exists && assetPriority(current.GOOS, current.Format) >= assetPriority(matched.GOOS, matched.Format) {
			continue
		}

		selectedAssets[matched.PlatformKey] = matched
		platforms[matched.PlatformKey] = updatePlatform{
			URL:      asset.BrowserDownloadURL,
			Size:     asset.Size,
			Checksum: checksum,
		}
	}

	if len(platforms) == 0 {
		return updateManifest{}, errors.New("no desktop release assets found")
	}

	return updateManifest{
		Version:      version,
		ReleaseDate:  publishedAt.UTC().Format(time.RFC3339),
		ReleaseNotes: release.Body,
		Platforms:    platforms,
		Mandatory:    false,
	}, nil
}

func normalizeVersion(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "v")
	return tag
}

func matchReleaseAsset(name string) (matchedReleaseAsset, bool) {
	matches := desktopAssetNamePattern.FindStringSubmatch(strings.TrimSpace(name))
	if len(matches) != 4 {
		return matchedReleaseAsset{}, false
	}

	goos := matches[1]
	goarch := matches[2]
	format := matches[3]
	if goos == "darwin" {
		return matchedReleaseAsset{
			PlatformKey: "macos-" + goarch,
			GOOS:        "darwin",
			Format:      format,
		}, true
	}
	return matchedReleaseAsset{
		PlatformKey: goos + "-" + goarch,
		GOOS:        goos,
		Format:      format,
	}, true
}

func assetPriority(goos, format string) int {
	switch goos {
	case "darwin", "linux":
		switch format {
		case "tar.gz":
			return 100
		case "dmg", "AppImage":
			return 50
		}
	case "windows":
		switch format {
		case "zip":
			return 100
		case "tar.gz":
			return 50
		}
	}
	return 0
}
