//go:build !windows

package workspace

import (
	"os"
	"strings"
)

func isHiddenDirEntry(name string, _ os.DirEntry, _ string) bool {
	return strings.HasPrefix(name, ".")
}
