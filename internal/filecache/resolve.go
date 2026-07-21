package filecache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

// ResolveFile resolves an existing regular file under file_cache_dir.
// Both the cache root and candidate are resolved through symlinks before the
// containment check.
func ResolveFile(cacheDir string, rawPath string, maxBytes int64) (string, error) {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return "", fmt.Errorf("file_cache_dir is required")
	}

	rawPath = pathutil.NormalizeFileCacheDirPath(strings.TrimSpace(rawPath))
	if rawPath == "" {
		return "", fmt.Errorf("file path is required")
	}

	cacheAbs, err := filepath.Abs(cacheDir)
	if err != nil {
		return "", err
	}
	cacheResolved, err := filepath.EvalSymlinks(cacheAbs)
	if err != nil {
		return "", err
	}
	cacheInfo, err := os.Stat(cacheResolved)
	if err != nil {
		return "", err
	}
	if !cacheInfo.IsDir() {
		return "", fmt.Errorf("file_cache_dir is not a directory: %s", cacheResolved)
	}

	path := rawPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(cacheAbs, path)
	}
	pathAbs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	pathResolved, err := filepath.EvalSymlinks(pathAbs)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(cacheResolved, pathResolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("refusing to send file outside file_cache_dir: %s", pathResolved)
	}

	info, err := os.Stat(pathResolved)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", pathResolved)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path is not a regular file: %s", pathResolved)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return "", fmt.Errorf("file too large to send (>%d bytes): %s", maxBytes, pathResolved)
	}
	return pathResolved, nil
}
