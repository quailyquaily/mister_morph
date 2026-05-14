package telegramutil

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type fileCacheEntry struct {
	Path    string
	ModTime time.Time
	Size    int64
}

func EnsureSecureCacheDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("empty dir")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	dir = abs

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink path: %s", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}

	return ensureCacheDirOwnershipAndPerms(dir, fi)
}

func CleanupFileCacheDir(dir string, maxAge time.Duration, maxFiles int, maxTotalBytes int64) error {
	return CleanupFileCacheDirWithProtected(dir, maxAge, maxFiles, maxTotalBytes, nil)
}

func CleanupFileCacheDirWithProtected(dir string, maxAge time.Duration, maxFiles int, maxTotalBytes int64, protected map[string]bool) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("missing dir")
	}
	if maxAge <= 0 && maxFiles <= 0 && maxTotalBytes <= 0 {
		return nil
	}
	now := time.Now()
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

	var kept []fileCacheEntry
	total := int64(0)

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Never follow symlinks.
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if maxAge > 0 && now.Sub(info.ModTime()) > maxAge && !isProtected(path) {
			_ = os.Remove(path)
			return nil
		}
		kept = append(kept, fileCacheEntry{
			Path:    path,
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
		total += info.Size()
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return walkErr
	}

	// Enforce max_files and max_total_bytes by removing oldest files first.
	sort.Slice(kept, func(i, j int) bool { return kept[i].ModTime.Before(kept[j].ModTime) })
	needPrune := func() bool {
		if maxFiles > 0 && len(kept) > maxFiles {
			return true
		}
		if maxTotalBytes > 0 && total > maxTotalBytes {
			return true
		}
		return false
	}
	for needPrune() && len(kept) > 0 {
		removeIndex := -1
		for i, entry := range kept {
			if !isProtected(entry.Path) {
				removeIndex = i
				break
			}
		}
		if removeIndex < 0 {
			break
		}
		old := kept[removeIndex]
		kept = append(kept[:removeIndex], kept[removeIndex+1:]...)
		total -= old.Size
		_ = os.Remove(old.Path)
	}

	// Best-effort remove empty dirs (bottom-up).
	var dirs []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		if filepath.Clean(d) == filepath.Clean(dir) {
			continue
		}
		_ = os.Remove(d)
	}
	return nil
}

func cleanProtectedPaths(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for raw, ok := range in {
		if !ok {
			continue
		}
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err == nil {
			path = abs
		}
		out[filepath.Clean(path)] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
