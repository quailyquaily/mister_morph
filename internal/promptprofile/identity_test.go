package promptprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/spf13/viper"
)

func TestAppendIdentityPromptBlock_LoadsIdentityBlock(t *testing.T) {
	workspaceDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", workspaceDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})
	identityPath := filepath.Join(workspaceDir, "IDENTITY.md")
	if err := os.WriteFile(identityPath, []byte("Name: test\nVibe: casual\n"), 0o644); err != nil {
		t.Fatalf("write identity file: %v", err)
	}

	spec := agent.DefaultPromptSpec()
	AppendIdentityPromptBlock(&spec, nil)

	found := false
	for _, block := range spec.Blocks {
		if block.Title == "Identity Profile" && strings.Contains(block.Content, "Vibe: casual") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Identity block not found in prompt blocks: %+v", spec.Blocks)
	}
}

func TestAppendIdentityPromptBlock_MissingIdentityFileDoesNotFail(t *testing.T) {
	workspaceDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", workspaceDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})

	spec := agent.DefaultPromptSpec()
	AppendIdentityPromptBlock(&spec, nil)
	for _, block := range spec.Blocks {
		if block.Title == "Identity Profile" {
			t.Fatalf("unexpected Identity block when file is missing")
		}
	}
}

func TestAppendIdentityPromptBlock_DraftIdentitySkipped(t *testing.T) {
	workspaceDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", workspaceDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})
	identityPath := filepath.Join(workspaceDir, "IDENTITY.md")
	content := "---\nstatus: draft\n---\n\n# IDENTITY.md\n\n- **Vibe:** test\n"
	if err := os.WriteFile(identityPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write identity file: %v", err)
	}

	spec := agent.DefaultPromptSpec()
	AppendIdentityPromptBlock(&spec, nil)
	for _, block := range spec.Blocks {
		if block.Title == "Identity Profile" {
			t.Fatalf("unexpected Identity block when status=draft")
		}
	}
}

func TestAppendSoulPromptBlock_LoadsSoulBlock(t *testing.T) {
	workspaceDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", workspaceDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})
	soulPath := filepath.Join(workspaceDir, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte("Core Truths:\nBe helpful.\n"), 0o644); err != nil {
		t.Fatalf("write soul file: %v", err)
	}

	spec := agent.DefaultPromptSpec()
	AppendSoulPromptBlock(&spec, nil)

	found := false
	for _, block := range spec.Blocks {
		if block.Title == "Soul Profile" && strings.Contains(block.Content, "Be helpful.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Soul block not found in prompt blocks: %+v", spec.Blocks)
	}
}

func TestAppendSoulPromptBlock_MissingSoulFileDoesNotFail(t *testing.T) {
	workspaceDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", workspaceDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})

	spec := agent.DefaultPromptSpec()
	AppendSoulPromptBlock(&spec, nil)
	for _, block := range spec.Blocks {
		if block.Title == "Soul Profile" {
			t.Fatalf("unexpected Soul block when file is missing")
		}
	}
}

func TestAppendSoulPromptBlock_DraftSoulSkipped(t *testing.T) {
	workspaceDir := t.TempDir()
	prevStateDir := viper.GetString("file_state_dir")
	viper.Set("file_state_dir", workspaceDir)
	t.Cleanup(func() {
		viper.Set("file_state_dir", prevStateDir)
	})
	soulPath := filepath.Join(workspaceDir, "SOUL.md")
	content := "---\nstatus: draft\n---\n\n# SOUL.md\n\n## Vibe\nTest\n"
	if err := os.WriteFile(soulPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write soul file: %v", err)
	}

	spec := agent.DefaultPromptSpec()
	AppendSoulPromptBlock(&spec, nil)
	for _, block := range spec.Blocks {
		if block.Title == "Soul Profile" {
			t.Fatalf("unexpected Soul block when status=draft")
		}
	}
}
