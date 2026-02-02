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
