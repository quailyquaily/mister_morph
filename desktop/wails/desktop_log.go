//go:build wailsdesktop

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const desktopLogFile = "desktop.log"

func desktopLogFilePath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mistermorph", "desktop", "logs", desktopLogFile), nil
}

func openDesktopLogFile() (*os.File, string, error) {
	path, err := desktopLogFilePath()
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, path, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, path, err
	}
	_, _ = fmt.Fprintf(file, "\n--- MisterMorph desktop start %s ---\n", time.Now().Format(time.RFC3339))
	return file, path, nil
}
