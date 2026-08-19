package statepaths

import (
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestConfiguredPaths(t *testing.T) {
	oldFileStateDir := viper.GetString("file_state_dir")
	oldJournalDirName := viper.GetString("journal.dir_name")
	oldSkillsDirName := viper.GetString("skills.dir_name")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldFileStateDir)
		viper.Set("journal.dir_name", oldJournalDirName)
		viper.Set("skills.dir_name", oldSkillsDirName)
	})

	root := t.TempDir()
	viper.Set("file_state_dir", root)
	viper.Set("journal.dir_name", "ignored-journal")
	viper.Set("skills.dir_name", "skills")

	if got, want := JournalDir(), filepath.Join(root, "journal"); got != want {
		t.Fatalf("JournalDir() = %q, want %q", got, want)
	}
	if got, want := DefaultSkillsRoots(), []string{filepath.Join(root, "skills")}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("DefaultSkillsRoots() = %#v, want %#v", got, want)
	}
}
