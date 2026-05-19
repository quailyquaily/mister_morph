package onboardingcheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectConfigPath(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("llm:\n  provider: openai\n  model: gpt-5.2\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		got := InspectConfigPath(path)
		if got.Status != StatusOK {
			t.Fatalf("Status = %q, want %q (%s)", got.Status, StatusOK, got.Error)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("llm: [\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		got := InspectConfigPath(path)
		if got.Status != StatusMalformed {
			t.Fatalf("Status = %q, want %q", got.Status, StatusMalformed)
		}
	})
}

func TestValidateIdentityMarkdown(t *testing.T) {
	if err := ValidateIdentityMarkdown("# IDENTITY\n\n```yaml\nname: Momo\ncreature: cat\nvibe: calm\nemoji: 🐈\n```\n"); err != nil {
		t.Fatalf("ValidateIdentityMarkdown() error = %v", err)
	}
	if err := ValidateIdentityMarkdown("- **Name:** Momo\n- **Creature:** cat\n- **Vibe:** calm\n- **Emoji:** 🐈\n"); err != nil {
		t.Fatalf("ValidateIdentityMarkdown() legacy error = %v", err)
	}
	if err := ValidateIdentityMarkdown("# IDENTITY\n\n```yaml\nname: [\n```\n"); err == nil {
		t.Fatalf("ValidateIdentityMarkdown() error = nil, want malformed")
	}
}

func TestValidateIdentityYAML(t *testing.T) {
	if err := ValidateIdentityYAML("name: Momo\ncreature: cat\nvibe: calm\nemoji: cat\n"); err != nil {
		t.Fatalf("ValidateIdentityYAML() error = %v", err)
	}
	if err := ValidateIdentityYAML("- name: Momo\n"); err == nil {
		t.Fatalf("ValidateIdentityYAML() error = nil, want malformed")
	}
}

func TestValidateSoulMarkdown(t *testing.T) {
	if err := ValidateSoulMarkdown("# SOUL.md\n\n## Core Truths\n- A\n\n## Boundaries\n- B\n\n## Vibe\n\nC\n"); err != nil {
		t.Fatalf("ValidateSoulMarkdown() error = %v", err)
	}
	if err := ValidateSoulMarkdown("# SOUL.md\n\n## Vibe\n\nC\n"); err == nil {
		t.Fatalf("ValidateSoulMarkdown() error = nil, want malformed")
	}
}
