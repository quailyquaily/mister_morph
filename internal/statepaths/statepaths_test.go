package statepaths

import (
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestJournalPaths(t *testing.T) {
	oldFileStateDir := viper.GetString("file_state_dir")
	oldJournalDirName := viper.GetString("journal.dir_name")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldFileStateDir)
		viper.Set("journal.dir_name", oldJournalDirName)
	})

	root := t.TempDir()
	viper.Set("file_state_dir", root)
	viper.Set("journal.dir_name", "journal")

	if got, want := JournalDir(), filepath.Join(root, "journal"); got != want {
		t.Fatalf("JournalDir() = %q, want %q", got, want)
	}
	if got, want := JournalEventsPath(), filepath.Join(root, "journal", "events.000000000000000001.jsonl"); got != want {
		t.Fatalf("JournalEventsPath() = %q, want %q", got, want)
	}
}
