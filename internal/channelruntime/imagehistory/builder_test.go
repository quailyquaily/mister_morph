package imagehistory

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
)

func TestBuildUsesWorkspaceAliasAndStableID(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	cacheDir := t.TempDir()
	imagePath := filepath.Join(workspaceDir, ".mistermorph", "images", "telegram", "chat_1", "a.png")
	writePNG(t, imagePath, 2, 3)

	input := Input{
		SourceMessageID:    "100",
		SourceAttachmentID: "file_unique_1",
		LocalPath:          imagePath,
		MIMEType:           "image/png; charset=binary",
	}
	images := Build([]Input{input}, pathroots.New(workspaceDir, cacheDir, ""))
	if len(images) != 1 {
		t.Fatalf("images len = %d, want 1", len(images))
	}
	img := images[0]
	if img.ID == "" {
		t.Fatalf("image id is empty")
	}
	if img.ContentSHA256 == "" {
		t.Fatalf("content_sha256 is empty")
	}
	if img.ID != "img_"+img.ContentSHA256[:16] {
		t.Fatalf("image id = %q, want content hash prefix %q", img.ID, "img_"+img.ContentSHA256[:16])
	}
	if img.Path != "workspace_dir/.mistermorph/images/telegram/chat_1/a.png" {
		t.Fatalf("path = %q, want workspace alias", img.Path)
	}
	if img.MIMEType != "image/png" {
		t.Fatalf("mime_type = %q, want image/png", img.MIMEType)
	}
	if img.Width != 2 || img.Height != 3 {
		t.Fatalf("dimensions = %dx%d, want 2x3", img.Width, img.Height)
	}
	if img.Bytes <= 0 {
		t.Fatalf("bytes = %d, want positive", img.Bytes)
	}
	if img.SourceMessageID != "100" || img.SourceAttachmentID != "file_unique_1" {
		t.Fatalf("source ids mismatch: %#v", img)
	}

	again := Build([]Input{input}, pathroots.New(workspaceDir, cacheDir, ""))
	if len(again) != 1 || again[0].ID != img.ID {
		t.Fatalf("stable id mismatch: first=%#v second=%#v", images, again)
	}
}

func TestBuildUsesContentHashIDAcrossSources(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	firstPath := filepath.Join(cacheDir, "telegram", "first.png")
	secondPath := filepath.Join(cacheDir, "slack", "second.png")
	writePNG(t, firstPath, 2, 2)
	raw, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(secondPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(secondPath, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	images := Build([]Input{
		{
			SourceMessageID:    "100",
			SourceAttachmentID: "tg_file_unique_1",
			LocalPath:          firstPath,
		},
		{
			SourceMessageID:    "1739667600.000100",
			SourceAttachmentID: "F111",
			LocalPath:          secondPath,
		},
	}, pathroots.New("", cacheDir, ""))
	if len(images) != 2 {
		t.Fatalf("images len = %d, want 2", len(images))
	}
	if images[0].ContentSHA256 == "" || images[0].ContentSHA256 != images[1].ContentSHA256 {
		t.Fatalf("content hashes mismatch: %#v", images)
	}
	if images[0].ID != images[1].ID {
		t.Fatalf("image ids mismatch for identical content: %#v", images)
	}
}

func TestBuildLeavesMissingAttachmentIDEmpty(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	imagePath := filepath.Join(cacheDir, "slack", "a.png")
	writePNG(t, imagePath, 1, 1)

	images := Build([]Input{{
		SourceMessageID: "1739667600.000100",
		LocalPath:       imagePath,
	}}, pathroots.New("", cacheDir, ""))
	if len(images) != 1 {
		t.Fatalf("images len = %d, want 1", len(images))
	}
	if images[0].SourceAttachmentID != "" {
		t.Fatalf("source attachment id = %q, want empty", images[0].SourceAttachmentID)
	}
	if images[0].Path != "file_cache_dir/slack/a.png" {
		t.Fatalf("path = %q, want file cache alias", images[0].Path)
	}
}

func TestDownloadDirUsesWorkspaceWhenAvailable(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	cacheDir := t.TempDir()
	got, err := DownloadDir(cacheDir, workspaceDir, chathistory.ChannelLine)
	if err != nil {
		t.Fatalf("DownloadDir() error = %v", err)
	}
	want := filepath.Join(workspaceDir, ".mistermorph", "images", chathistory.ChannelLine)
	if got != want {
		t.Fatalf("DownloadDir() = %q, want %q", got, want)
	}

	got, err = DownloadDir(cacheDir, "", chathistory.ChannelLine)
	if err != nil {
		t.Fatalf("DownloadDir() fallback error = %v", err)
	}
	want = filepath.Join(cacheDir, chathistory.ChannelLine)
	if got != want {
		t.Fatalf("DownloadDir() fallback = %q, want %q", got, want)
	}
}

func TestWithDescriptionCopiesImages(t *testing.T) {
	t.Parallel()

	images := []chathistory.ChatHistoryImage{{ID: "img_1"}}
	got := WithDescription(images, "a diagram", "agent_final")
	if len(got) != 1 {
		t.Fatalf("images len = %d, want 1", len(got))
	}
	if got[0].Description != "a diagram" || got[0].DescriptionSource != "agent_final" {
		t.Fatalf("description mismatch: %#v", got[0])
	}
	if images[0].Description != "" {
		t.Fatalf("source image mutated: %#v", images[0])
	}
}

func writePNG(t *testing.T, path string, width int, height int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer f.Close()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
}
