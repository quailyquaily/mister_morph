package filecache

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Limits bounds a file cache by age, file count, and total regular-file size.
// A non-positive field disables that individual limit.
type Limits struct {
	MaxAge        time.Duration
	MaxFiles      int
	MaxTotalBytes int64
}

type cleanupEntry struct {
	path    string
	modTime time.Time
	size    int64
}

// Cleanup removes expired files first, then removes the oldest remaining files
// until the count and byte limits are satisfied. Protected files are never
// removed during this call.
func Cleanup(dir string, limits Limits, protected map[string]bool) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("missing dir")
	}
	if limits.MaxAge <= 0 && limits.MaxFiles <= 0 && limits.MaxTotalBytes <= 0 {
		return nil
	}

	protected = cleanProtectedPaths(protected)
	isProtected := func(path string) bool {
		if len(protected) == 0 {
			return false
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return protected[filepath.Clean(path)]
		}
		return protected[filepath.Clean(abs)]
	}

	now := time.Now()
	kept := make([]cleanupEntry, 0)
	var total int64
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if limits.MaxAge > 0 && now.Sub(info.ModTime()) > limits.MaxAge && !isProtected(path) {
			return os.Remove(path)
		}
		kept = append(kept, cleanupEntry{path: path, modTime: info.ModTime(), size: info.Size()})
		total += info.Size()
		return nil
	})
	if walkErr != nil {
		if os.IsNotExist(walkErr) {
			return nil
		}
		return walkErr
	}

	sort.Slice(kept, func(i, j int) bool {
		if kept[i].modTime.Equal(kept[j].modTime) {
			return kept[i].path < kept[j].path
		}
		return kept[i].modTime.Before(kept[j].modTime)
	})
	limitsExceeded := func() bool {
		return limits.MaxFiles > 0 && len(kept) > limits.MaxFiles ||
			limits.MaxTotalBytes > 0 && total > limits.MaxTotalBytes
	}
	for limitsExceeded() && len(kept) > 0 {
		removeIndex := -1
		for index := range kept {
			if !isProtected(kept[index].path) {
				removeIndex = index
				break
			}
		}
		if removeIndex < 0 {
			break
		}
		oldest := kept[removeIndex]
		if err := os.Remove(oldest.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		kept = append(kept[:removeIndex], kept[removeIndex+1:]...)
		total -= oldest.size
	}

	removeEmptyCacheDirs(dir)
	return nil
}

func cleanProtectedPaths(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for raw, keep := range in {
		if !keep {
			continue
		}
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		out[filepath.Clean(path)] = true
	}
	return out
}

func removeEmptyCacheDirs(root string) {
	dirs := make([]string, 0)
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		if filepath.Clean(dir) != filepath.Clean(root) {
			_ = os.Remove(dir)
		}
	}
}
