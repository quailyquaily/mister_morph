package telegram

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
)

func TestCollectDownloadedImageAttachmentsPreservesSourceIDs(t *testing.T) {
	files := []telegramDownloadedFile{{
		Kind:               "photo",
		Path:               "/tmp/p1.jpg",
		SourceMessageID:    "100",
		SourceAttachmentID: "file_unique_1",
	}}
	got := collectDownloadedImageAttachments(files, 3)
	if len(got) != 1 {
		t.Fatalf("collectDownloadedImageAttachments() len = %d, want 1", len(got))
	}
	if got[0].SourceMessageID != "100" || got[0].SourceAttachmentID != "file_unique_1" {
		t.Fatalf("source ids mismatch: %#v", got[0])
	}
}

func TestAppendDownloadedFilesToTaskUsesAliasPath(t *testing.T) {
	cacheDir := t.TempDir()
	filePath := filepath.Join(cacheDir, "telegram", "chat_1", "a.png")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := appendDownloadedFilesToTask("process", []telegramDownloadedFile{{
		Kind:         "photo",
		OriginalName: "a.png",
		Path:         filePath,
	}}, pathroots.New("", cacheDir, ""))
	if !strings.Contains(got, "file_cache_dir/telegram/chat_1/a.png") {
		t.Fatalf("alias path missing: %q", got)
	}
	if strings.Contains(got, cacheDir) {
		t.Fatalf("local cache path leaked: %q", got)
	}
}
