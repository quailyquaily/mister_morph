package consolecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConsoleRuntimeConfigValidatesWorkspaceDir(t *testing.T) {
	workspaceDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("workspace_dir: "+workspaceDir+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	reader, err := loadConsoleRuntimeConfig(configPath, nil)
	if err != nil {
		t.Fatalf("loadConsoleRuntimeConfig() error = %v", err)
	}
	if got := reader.GetString("workspace_dir"); got != workspaceDir {
		t.Fatalf("workspace_dir = %q, want %q", got, workspaceDir)
	}
}

func TestLoadConsoleRuntimeConfigRejectsInvalidWorkspaceDir(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("workspace_dir: "+missingDir+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	_, err := loadConsoleRuntimeConfig(configPath, nil)
	if err == nil || !strings.Contains(err.Error(), "workspace dir does not exist") {
		t.Fatalf("loadConsoleRuntimeConfig() error = %v, want missing workspace error", err)
	}
}

func TestLoadConsoleRuntimeConfigUsesWorkspaceDirEnv(t *testing.T) {
	workspaceDir := t.TempDir()
	t.Setenv("MISTER_MORPH_WORKSPACE_DIR", workspaceDir)

	reader, err := loadConsoleRuntimeConfig("", nil)
	if err != nil {
		t.Fatalf("loadConsoleRuntimeConfig() error = %v", err)
	}
	if got := reader.GetString("workspace_dir"); got != workspaceDir {
		t.Fatalf("workspace_dir = %q, want %q", got, workspaceDir)
	}
}
