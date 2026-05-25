//go:build windows

package workspace

import (
	"os"
	"strings"
	"syscall"
)

func isHiddenDirEntry(name string, entry os.DirEntry, absPath string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if info, err := entry.Info(); err == nil {
		if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
			return data.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
		}
	}
	pointer, err := syscall.UTF16PtrFromString(absPath)
	if err != nil {
		return false
	}
	attrs, err := syscall.GetFileAttributes(pointer)
	return err == nil && attrs&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}
