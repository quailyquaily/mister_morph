package slack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

func resolveFileCachePath(cacheDir string, rawPath string, maxBytes int64) (string, error) {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return "", fmt.Errorf("file_cache_dir is required")
	}
	rawPath = pathutil.NormalizeFileCacheDirPath(strings.TrimSpace(rawPath))

	p := rawPath
	if !filepath.IsAbs(p) {
		p = filepath.Join(cacheDir, p)
	}
	p = filepath.Clean(p)

	cacheAbs, err := filepath.Abs(cacheDir)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}

	// Resolve symlinks before containment check so links under file_cache_dir
	// cannot escape to arbitrary filesystem locations.
	cacheResolved := cacheAbs
	if resolved, resolveErr := filepath.EvalSymlinks(cacheAbs); resolveErr == nil {
		cacheResolved = resolved
	} else if !os.IsNotExist(resolveErr) {
		return "", resolveErr
	}
	pathResolved, err := filepath.EvalSymlinks(pathAbs)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(cacheResolved, pathResolved)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", fmt.Errorf("refusing to send file outside file_cache_dir: %s", pathResolved)
	}

	st, err := os.Stat(pathResolved)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", pathResolved)
	}
	if maxBytes > 0 && st.Size() > maxBytes {
		return "", fmt.Errorf("file too large to send (>%d bytes): %s", maxBytes, pathResolved)
	}
	return pathResolved, nil
}
