package imageinput

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

func AppendImagePathNotes(content string, imagePaths []string, fileCacheDir string) string {
	if len(imagePaths) == 0 {
		return strings.TrimSpace(content)
	}
	cacheDir := strings.TrimSpace(fileCacheDir)
	if cacheDir == "" {
		return strings.TrimSpace(content)
	}
	cacheAbs, err := filepath.Abs(pathutil.ExpandHomePath(cacheDir))
	if err != nil {
		return strings.TrimSpace(content)
	}

	lines := make([]string, 0, len(imagePaths)+1)
	lines = append(lines, "Local image files available to image_edit:")
	seen := map[string]bool{}
	for _, rawPath := range imagePaths {
		path := strings.TrimSpace(rawPath)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		pathAbs, err := filepath.Abs(pathutil.ExpandHomePath(path))
		if err != nil || !pathutil.IsWithinDir(cacheAbs, pathAbs) {
			continue
		}
		rel, err := filepath.Rel(cacheAbs, pathAbs)
		if err != nil || strings.TrimSpace(rel) == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		lines = append(lines, fmt.Sprintf("- attached image %d: %s", len(lines), filepath.ToSlash(filepath.Join("file_cache_dir", rel))))
	}
	if len(lines) == 1 {
		return strings.TrimSpace(content)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return strings.Join(lines, "\n")
	}
	return content + "\n\n" + strings.Join(lines, "\n")
}

func AppendImageMetadataNotes(content string, images []chathistory.ChatHistoryImage) string {
	if len(images) == 0 {
		return strings.TrimSpace(content)
	}

	lines := make([]string, 0, len(images)+1)
	lines = append(lines, "Local image files available to image_edit:")
	seen := map[string]bool{}
	for _, img := range images {
		id := strings.TrimSpace(img.ID)
		path := strings.TrimSpace(img.Path)
		if id == "" || path == "" || seen[id+"|"+path] {
			continue
		}
		seen[id+"|"+path] = true
		lines = append(lines, fmt.Sprintf("- %s: %s", id, path))
	}
	if len(lines) == 1 {
		return strings.TrimSpace(content)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return strings.Join(lines, "\n")
	}
	return content + "\n\n" + strings.Join(lines, "\n")
}
