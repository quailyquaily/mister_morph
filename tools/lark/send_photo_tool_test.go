package lark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubLarkAPI struct {
	chatID    string
	messageID string
	filePath  string
	filename  string
	caption   string
	emojiType string
}

func (s *stubLarkAPI) SendFile(_ context.Context, chatID string, filePath string, filename string, caption string) error {
	s.chatID = chatID
	s.filePath = filePath
	s.filename = filename
	s.caption = caption
	return nil
}

func (s *stubLarkAPI) SendPhoto(_ context.Context, chatID string, filePath string, filename string, caption string) error {
	s.chatID = chatID
	s.filePath = filePath
	s.filename = filename
	s.caption = caption
	return nil
}

func (s *stubLarkAPI) SendVoice(_ context.Context, chatID string, filePath string, filename string) error {
	s.chatID = chatID
	s.filePath = filePath
	s.filename = filename
	return nil
}

func (s *stubLarkAPI) SetEmojiReaction(_ context.Context, messageID string, emojiType string) error {
	s.messageID = messageID
	s.emojiType = emojiType
	return nil
}

func TestSendPhotoToolExecute(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	imagePath := filepath.Join(cacheDir, "x y?.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	api := &stubLarkAPI{}
	tool := NewSendPhotoTool(api, "oc_123", cacheDir, 1024)
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
	if api.chatID != "oc_123" {
		t.Fatalf("chat_id = %q, want oc_123", api.chatID)
	}
	if api.filePath != imagePath {
		t.Fatalf("file_path = %q, want %q", api.filePath, imagePath)
	}
	if api.filename != "x_y_.png" {
		t.Fatalf("filename = %q, want x_y_.png", api.filename)
	}
	if api.caption != "hello" {
		t.Fatalf("caption = %q, want hello", api.caption)
	}
}

func TestSendPhotoToolExecuteRejectsOutsideCacheDir(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "image.png")
	if err := os.WriteFile(outsidePath, []byte("png"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	api := &stubLarkAPI{}
	tool := NewSendPhotoTool(api, "oc_123", cacheDir, 1024)
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

func TestReactToolDefaultsToCurrentMessageAndMapsEmoji(t *testing.T) {
	t.Parallel()

	api := &stubLarkAPI{}
	tool := NewReactTool(api, "om_123")
	got, err := tool.Execute(context.Background(), map[string]any{
		"emoji": "👍",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got != "reacted with THUMBSUP" {
		t.Fatalf("result = %q, want reacted with THUMBSUP", got)
	}
	if api.messageID != "om_123" {
		t.Fatalf("message_id = %q, want om_123", api.messageID)
	}
	if api.emojiType != "THUMBSUP" {
		t.Fatalf("emoji_type = %q, want THUMBSUP", api.emojiType)
	}
	if tool.LastReaction() == nil || tool.LastReaction().EmojiType != "THUMBSUP" {
		t.Fatalf("last reaction = %#v, want THUMBSUP", tool.LastReaction())
	}
}
