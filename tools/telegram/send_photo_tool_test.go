package telegram

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubPhotoAPI struct {
	chatID          int64
	messageThreadID int64
	filePath        string
	filename        string
	caption         string
}

func (s *stubPhotoAPI) SendDocument(context.Context, int64, int64, string, string, string) error {
	return nil
}

func (s *stubPhotoAPI) SendPhoto(_ context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error {
	s.chatID = chatID
	s.messageThreadID = messageThreadID
	s.filePath = filePath
	s.filename = filename
	s.caption = caption
	return nil
}

func (s *stubPhotoAPI) SendVoice(context.Context, int64, int64, string, string, string) error {
	return nil
}

func (s *stubPhotoAPI) SetEmojiReaction(context.Context, int64, int64, string, *bool) error {
	return nil
}

func TestSendPhotoToolExecute(t *testing.T) {
	cacheDir := t.TempDir()
	imagePath := filepath.Join(cacheDir, "x y?.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	api := &stubPhotoAPI{}
	tool := NewSendPhotoTool(api, 42, 901, cacheDir, 1024)
	got, err := tool.Execute(context.Background(), map[string]any{
		"path":    "x y?.png",
		"caption": "hello",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "sent photo: x_y_.png" {
		t.Fatalf("result = %q, want %q", got, "sent photo: x_y_.png")
	}
	if api.chatID != 42 {
		t.Fatalf("chat_id = %d, want 42", api.chatID)
	}
	if api.messageThreadID != 901 {
		t.Fatalf("message_thread_id = %d, want 901", api.messageThreadID)
	}
	if api.filePath != imagePath {
		t.Fatalf("file_path = %q, want %q", api.filePath, imagePath)
	}
	if api.filename != "x_y_.png" {
		t.Fatalf("filename = %q, want %q", api.filename, "x_y_.png")
	}
	if api.caption != "hello" {
		t.Fatalf("caption = %q, want hello", api.caption)
	}
}

func TestSendPhotoToolExecuteRejectsOutsideCacheDir(t *testing.T) {
	cacheDir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "image.png")
	if err := os.WriteFile(outsidePath, []byte("png"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	api := &stubPhotoAPI{}
	tool := NewSendPhotoTool(api, 42, 0, cacheDir, 1024)
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": outsidePath,
	})
	if err == nil {
		t.Fatalf("Execute() error = nil, want outside-file error")
	}
	if !strings.Contains(err.Error(), "outside file_cache_dir") {
		t.Fatalf("error = %v, want outside file_cache_dir", err)
	}
}
