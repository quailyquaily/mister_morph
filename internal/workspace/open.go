package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

var openPathRunner = openPathWithSystemDefault

func ResolveAttachedItemPath(workspaceDir string, relPath string) (string, error) {
	rootDir, err := ValidateDir(workspaceDir, nil)
	if err != nil {
		return "", err
	}
	return resolveAttachedItemPath(rootDir, relPath)
}

func OpenPath(rawPath string) error {
	target := strings.TrimSpace(rawPath)
	if target == "" {
		return fmt.Errorf("path is required")
	}
	absPath, err := filepath.Abs(pathutil.ExpandHomePath(target))
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", absPath)
		}
		return err
	}
	return openPathRunner(absPath)
}

func resolveAttachedItemPath(rootDir string, relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" || relPath == "." {
		return "", fmt.Errorf("workspace item path is required")
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("workspace item path must be relative")
	}
	cleanPath := filepath.Clean(relPath)
	if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace item path is outside the attached directory")
	}
	targetPath, err := filepath.Abs(filepath.Join(rootDir, cleanPath))
	if err != nil {
		return "", err
	}
	if filepath.Clean(targetPath) != filepath.Clean(rootDir) && !pathutil.IsWithinDir(rootDir, targetPath) {
		return "", fmt.Errorf("workspace item path is outside the attached directory")
	}
	if _, err := os.Stat(targetPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("workspace item does not exist: %s", cleanPath)
		}
		return "", err
	}
	return targetPath, nil
}

func openPathWithSystemDefault(absPath string) error {
	cmd, err := openPathCommand(absPath)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

func openPathCommand(absPath string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", absPath), nil
	case "windows":
		return exec.Command("cmd", "/c", "start", "", absPath), nil
	default:
		return exec.Command("xdg-open", absPath), nil
	}
}
