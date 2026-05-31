package imageinput

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
)

func TestAppendImagePathNotesUsesFileCacheAlias(t *testing.T) {
	cacheDir := t.TempDir()
	imagePath := filepath.Join(cacheDir, "telegram", "a.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := AppendImagePathNotes("edit this", []string{imagePath}, cacheDir)
	if !strings.Contains(got, "edit this") {
		t.Fatalf("content missing: %q", got)
	}
	if !strings.Contains(got, "file_cache_dir/telegram/a.png") {
		t.Fatalf("alias path missing: %q", got)
	}
	if strings.Contains(got, cacheDir) {
		t.Fatalf("local cache path leaked: %q", got)
	}
}

func TestAppendImageMetadataNotesIncludesImageIDAndAlias(t *testing.T) {
	t.Parallel()

	got := AppendImageMetadataNotes("edit this", []chathistory.ChatHistoryImage{{
		ID:   "img_abc123",
		Path: "workspace_dir/.mistermorph/images/slack/a.png",
	}})
	if !strings.Contains(got, "edit this") {
		t.Fatalf("content missing: %q", got)
	}
	if !strings.Contains(got, "img_abc123") {
		t.Fatalf("image id missing: %q", got)
	}
	if !strings.Contains(got, "workspace_dir/.mistermorph/images/slack/a.png") {
		t.Fatalf("alias path missing: %q", got)
	}
}
