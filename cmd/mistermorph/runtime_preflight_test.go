package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestRuntimeFilePreflightMigratesLegacyPersona(t *testing.T) {
	initViperDefaults()

	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", stateDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
	})

	legacyIdentity := strings.Join([]string{
		"```yaml",
		"name: Existing",
		"creature: Human",
		"vibe: Keep this",
		"emoji: test",
		"```",
		"",
	}, "\n")
	legacySoul := strings.Join([]string{
		"# SOUL.md",
		"",
		"## Core Truths",
		"- keep",
		"",
		"## Boundaries",
		"- keep",
		"",
		"## Vibe",
		"Keep this voice.",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(stateDir, "IDENTITY.md"), []byte(legacyIdentity), 0o600); err != nil {
		t.Fatalf("write legacy identity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "SOUL.md"), []byte(legacySoul), 0o600); err != nil {
		t.Fatalf("write legacy soul: %v", err)
	}

	if err := runRuntimeFilePreflight(io.Discard); err != nil {
		t.Fatalf("runRuntimeFilePreflight() error = %v", err)
	}

	nextIdentity, err := os.ReadFile(filepath.Join(stateDir, "persona", "identity.yaml"))
	if err != nil {
		t.Fatalf("read migrated identity: %v", err)
	}
	if !strings.Contains(string(nextIdentity), "name: Existing") {
		t.Fatalf("identity was not migrated: %q", string(nextIdentity))
	}
	nextSoul, err := os.ReadFile(filepath.Join(stateDir, "persona", "soul.md"))
	if err != nil {
		t.Fatalf("read migrated soul: %v", err)
	}
	if !strings.Contains(string(nextSoul), "Keep this voice.") {
		t.Fatalf("soul was not migrated: %q", string(nextSoul))
	}
}
