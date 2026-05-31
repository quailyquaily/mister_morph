package telegram

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
)

func TestCollectDownloadedImagePaths(t *testing.T) {
	files := []telegramDownloadedFile{
		{Kind: "document", MimeType: "application/pdf", Path: "/tmp/a.pdf"},
		{Kind: "photo", Path: "/tmp/p1.jpg"},
		{Kind: "document", MimeType: "image/png", Path: "/tmp/p2.png"},
		{Kind: "document", OriginalName: "x.webp", Path: "/tmp/p3.webp"},
		{Kind: "photo", Path: "/tmp/p1.jpg"},
	}
	got := collectDownloadedImagePaths(files, 3)
	if len(got) != 3 {
		t.Fatalf("collectDownloadedImagePaths() len = %d, want 3", len(got))
	}
	if got[0] != "/tmp/p1.jpg" || got[1] != "/tmp/p2.png" || got[2] != "/tmp/p3.webp" {
		t.Fatalf("collectDownloadedImagePaths() = %#v", got)
	}
}

func TestCollectDownloadedImagePathsMaxZero(t *testing.T) {
	files := []telegramDownloadedFile{{Kind: "photo", Path: "/tmp/p1.jpg"}}
	got := collectDownloadedImagePaths(files, 0)
	if len(got) != 0 {
		t.Fatalf("collectDownloadedImagePaths(max=0) = %#v, want nil/empty", got)
	}
}

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
