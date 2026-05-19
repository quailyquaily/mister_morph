package promptprofile

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/spf13/viper"
)

func TestApplyPersonaIdentityDoesNotMigrateLegacyFiles(t *testing.T) {
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
		"vibe: Direct",
		"emoji: test",
		"```",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(stateDir, "IDENTITY.md"), []byte(legacyIdentity), 0o600); err != nil {
		t.Fatalf("write legacy identity: %v", err)
	}

	spec := agent.PromptSpec{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ApplyPersonaIdentity(&spec, logger)

	if !strings.Contains(spec.Identity, "Existing") {
		t.Fatalf("legacy identity was not read: %q", spec.Identity)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "persona", "identity.yaml")); !os.IsNotExist(err) {
		t.Fatalf("ApplyPersonaIdentity should not write migrated identity, stat err=%v", err)
	}
}
