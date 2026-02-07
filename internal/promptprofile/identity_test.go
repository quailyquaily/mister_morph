package promptprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/spf13/viper"
)

func TestApplyPersonaIdentity_ReplacesIdentityFromFiles(t *testing.T) {
	stateDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", stateDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})

	if err := os.WriteFile(filepath.Join(stateDir, "IDENTITY.md"), []byte("Name: Nova\nVibe: calm\n"), 0o644); err != nil {
		t.Fatalf("write identity file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "SOUL.md"), []byte("Core Truths:\n- Be useful.\n"), 0o644); err != nil {
		t.Fatalf("write soul file: %v", err)
	}

	spec := agent.DefaultPromptSpec()
	orig := spec.Identity
	ApplyPersonaIdentity(&spec, nil)

	if spec.Identity == orig {
		t.Fatalf("identity should be replaced")
	}
	if !strings.Contains(spec.Identity, "## IDENTITY.md") {
		t.Fatalf("identity section missing: %s", spec.Identity)
	}
	if !strings.Contains(spec.Identity, "## SOUL.md") {
		t.Fatalf("soul section missing: %s", spec.Identity)
	}
	if len(spec.Blocks) != 0 {
		t.Fatalf("should not add prompt blocks")
	}
}

func TestApplyPersonaIdentity_MissingFilesNoChange(t *testing.T) {
	stateDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", stateDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})

	spec := agent.DefaultPromptSpec()
	orig := spec.Identity
	ApplyPersonaIdentity(&spec, nil)
	if spec.Identity != orig {
		t.Fatalf("identity should remain unchanged when files missing")
	}
}

func TestApplyPersonaIdentity_DraftFilesSkipped(t *testing.T) {
	stateDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", stateDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})

	identityDraft := "---\nstatus: draft\n---\n\n# IDENTITY\nName: draft\n"
	soulDraft := "---\nstatus: draft\n---\n\n# SOUL\nDraft\n"
	if err := os.WriteFile(filepath.Join(stateDir, "IDENTITY.md"), []byte(identityDraft), 0o644); err != nil {
		t.Fatalf("write identity file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "SOUL.md"), []byte(soulDraft), 0o644); err != nil {
		t.Fatalf("write soul file: %v", err)
	}

	spec := agent.DefaultPromptSpec()
	orig := spec.Identity
	ApplyPersonaIdentity(&spec, nil)
	if spec.Identity != orig {
		t.Fatalf("identity should remain unchanged when docs are draft")
	}
}

func TestLegacyWrappers(t *testing.T) {
	stateDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", stateDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})

	if err := os.WriteFile(filepath.Join(stateDir, "IDENTITY.md"), []byte("Name: test"), 0o644); err != nil {
		t.Fatalf("write identity file: %v", err)
	}
	spec := agent.DefaultPromptSpec()
	AppendIdentityPromptBlock(&spec, nil)
	if !strings.Contains(spec.Identity, "## IDENTITY.md") {
		t.Fatalf("legacy wrapper should apply persona identity")
	}
}
