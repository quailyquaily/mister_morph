package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestInstallWritesIdentityAndSoulUnderStateDir(t *testing.T) {
	initViperDefaults()

	stateDir := t.TempDir()
	workspaceDir := t.TempDir()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workspaceDir); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	cmd := newInstallCmd()
	cmd.SetArgs([]string{"--yes", stateDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install command failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(stateDir, "persona", "identity.yaml")); err != nil {
		t.Fatalf("persona/identity.yaml should exist under state dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "persona", "soul.md")); err != nil {
		t.Fatalf("persona/soul.md should exist under state dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "HEARTBEAT.md")); err != nil {
		t.Fatalf("HEARTBEAT.md should exist under state dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "cron.yaml")); err != nil {
		t.Fatalf("cron.yaml should exist under state dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "SCRIPTS.md")); !os.IsNotExist(err) {
		t.Fatalf("SCRIPTS.md should not be created during install, err=%v", err)
	}
}

func TestInstallUsesConfiguredStateDirWhenArgMissing(t *testing.T) {
	initViperDefaults()

	stateDir := filepath.Join(t.TempDir(), "configured-state")
	workspaceDir := t.TempDir()

	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", stateDir)
	t.Cleanup(func() {
		if prevStateDir == "" {
			viper.Set("file_state_dir", nil)
			return
		}
		viper.Set("file_state_dir", prevStateDir)
	})

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workspaceDir); err != nil {
		t.Fatalf("chdir workspace: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	cmd := newInstallCmd()
	cmd.SetArgs([]string{"--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install command failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(stateDir, "persona", "identity.yaml")); err != nil {
		t.Fatalf("persona/identity.yaml should exist under configured file_state_dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "persona", "soul.md")); err != nil {
		t.Fatalf("persona/soul.md should exist under configured file_state_dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "HEARTBEAT.md")); err != nil {
		t.Fatalf("HEARTBEAT.md should exist under configured file_state_dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "cron.yaml")); err != nil {
		t.Fatalf("cron.yaml should exist under configured file_state_dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "SCRIPTS.md")); !os.IsNotExist(err) {
		t.Fatalf("SCRIPTS.md should not be created during install, err=%v", err)
	}
}

func TestLoadIdentityTemplate(t *testing.T) {
	body, err := loadIdentityTemplate()
	if err != nil {
		t.Fatalf("loadIdentityTemplate() error = %v", err)
	}
	if body == "" {
		t.Fatalf("expected non-empty IDENTITY template")
	}
	if !strings.Contains(body, "name_alts: []") {
		t.Fatalf("identity template seems invalid")
	}
}

func TestLoadSoulTemplate(t *testing.T) {
	body, err := loadSoulTemplate()
	if err != nil {
		t.Fatalf("loadSoulTemplate() error = %v", err)
	}
	if body == "" {
		t.Fatalf("expected non-empty SOUL template")
	}
	if !strings.Contains(body, "# soul.md - Who You Are") {
		t.Fatalf("SOUL template seems invalid")
	}
}

func TestLoadCronTemplate(t *testing.T) {
	body, err := loadCronTemplate()
	if err != nil {
		t.Fatalf("loadCronTemplate() error = %v", err)
	}
	if body == "" {
		t.Fatalf("expected non-empty cron template")
	}
	if !strings.Contains(body, "version: 1") || !strings.Contains(body, "tasks: []") {
		t.Fatalf("cron template seems invalid")
	}
}

func TestInstallCommandExposesYesFlag(t *testing.T) {
	cmd := newInstallCmd()
	flag := cmd.Flags().Lookup("yes")
	if flag == nil {
		t.Fatalf("expected --yes flag to exist")
	}
	if flag.Shorthand != "y" {
		t.Fatalf("expected --yes shorthand to be -y, got %q", flag.Shorthand)
	}
}
