package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

func ExpandHomePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return filepath.Clean(p)
		}
		if p == "~" {
			return filepath.Clean(home)
		}
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(p, "~/")))
	}
	return filepath.Clean(p)
}

func NormalizeFileCacheDirPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	trimmed := strings.TrimLeft(p, "/\\")
	if strings.HasPrefix(trimmed, "file_cache_dir/") || strings.HasPrefix(trimmed, "file_cache_dir\\") {
		trimmed = strings.TrimPrefix(trimmed, "file_cache_dir/")
		trimmed = strings.TrimPrefix(trimmed, "file_cache_dir\\")
		return strings.TrimLeft(trimmed, "/\\")
	}
	return p
}

func ResolveStateDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "~/.morph"
	}
	return ExpandHomePath(dir)
}

func ResolveStateChildDir(stateDir string, name string, defaultName string) string {
	base := ResolveStateDir(stateDir)
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(defaultName)
	}
	if name == "" {
		return filepath.Clean(base)
	}
	return filepath.Clean(filepath.Join(base, name))
}

func ResolveStateFile(stateDir string, filename string) string {
	base := ResolveStateDir(stateDir)
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return filepath.Clean(base)
	}
	return filepath.Clean(filepath.Join(base, filename))
}

func IsWithinDir(baseAbs string, candAbs string) bool {
	baseAbs = filepath.Clean(baseAbs)
	candAbs = filepath.Clean(candAbs)
	rel, err := filepath.Rel(baseAbs, candAbs)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}
