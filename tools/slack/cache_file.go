package slack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

func resolveFileCachePath(cacheDir string, rawPath string, maxBytes int64) (string, error) {
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
	rel, err := filepath.Rel(cacheAbs, pathAbs)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", fmt.Errorf("refusing to send file outside file_cache_dir: %s", pathAbs)
	}

	st, err := os.Stat(pathAbs)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", pathAbs)
	}
	if maxBytes > 0 && st.Size() > maxBytes {
		return "", fmt.Errorf("file too large to send (>%d bytes): %s", maxBytes, pathAbs)
	}
	return pathAbs, nil
}
