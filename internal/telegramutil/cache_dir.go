package telegramutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
