package personamigrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMigratesLegacyIdentityAndSoul(t *testing.T) {
	stateDir := t.TempDir()
	identity := "# IDENTITY.md\n\n```yaml\nname: Momo\ncreature: cat\nvibe: calm\nemoji: cat\nunknown: yes\n```\n"
	if err := os.WriteFile(filepath.Join(stateDir, "IDENTITY.md"), []byte(identity), 0o600); err != nil {
		t.Fatalf("WriteFile() identity error = %v", err)
	}
	soul := "# SOUL.md\n\n## Core Truths\n- A\n\n## Boundaries\n- B\n\n## Vibe\n\nC\n"
	if err := os.WriteFile(filepath.Join(stateDir, "SOUL.md"), []byte(soul), 0o600); err != nil {
		t.Fatalf("WriteFile() soul error = %v", err)
	}

	result := Run(stateDir)
	if err := result.Err(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.IdentityMigrated || !result.SoulMigrated {
		t.Fatalf("migration result = %#v", result)
	}
	rawIdentity, err := os.ReadFile(filepath.Join(stateDir, "persona", "identity.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() identity error = %v", err)
	}
	if got := string(rawIdentity); strings.Contains(got, "```") || !strings.Contains(got, "unknown: yes") {
		t.Fatalf("identity.yaml content = %q", got)
	}
	rawSoul, err := os.ReadFile(filepath.Join(stateDir, "persona", "soul.md"))
	if err != nil {
		t.Fatalf("ReadFile() soul error = %v", err)
	}
	if !strings.Contains(string(rawSoul), "## Core Truths") {
		t.Fatalf("soul.md content = %q", string(rawSoul))
	}
}

func TestRunDoesNotOverwriteCanonicalIdentity(t *testing.T) {
	stateDir := t.TempDir()
	personaDir := filepath.Join(stateDir, "persona")
	if err := os.MkdirAll(personaDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(personaDir, "identity.yaml"), []byte("name: New\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() canonical error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "IDENTITY.md"), []byte("# broken"), 0o600); err != nil {
		t.Fatalf("WriteFile() legacy error = %v", err)
	}

	result := Run(stateDir)
	if err := result.Err(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(personaDir, "identity.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(raw) != "name: New\n" {
		t.Fatalf("identity.yaml overwritten: %q", string(raw))
	}
}
