package line

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
)

var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func TestBuildLineCurrentMessageWithImageParts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	if err := os.WriteFile(path, tinyPNG, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	msg, err := buildLineCurrentMessage("hello", "gpt-5.2", []string{path}, nil)
	if err != nil {
		t.Fatalf("buildLineCurrentMessage() error = %v", err)
	}
	if len(msg.Parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(msg.Parts))
	}
	if msg.Parts[0].Type != "text" {
		t.Fatalf("first part type = %q, want text", msg.Parts[0].Type)
	}
	if msg.Parts[1].Type != "image_base64" {
		t.Fatalf("second part type = %q, want image_base64", msg.Parts[1].Type)
	}
	if msg.Parts[1].MIMEType != "image/png" {
		t.Fatalf("second part mime = %q, want image/png", msg.Parts[1].MIMEType)
	}
	raw, err := base64.StdEncoding.DecodeString(msg.Parts[1].DataBase64)
	if err != nil {
		t.Fatalf("decode image base64: %v", err)
	}
	if string(raw) != string(tinyPNG) {
		t.Fatalf("image payload mismatch")
	}
}

func TestBuildLineCurrentMessageUnsupportedModel(t *testing.T) {
	t.Parallel()

	msg, err := buildLineCurrentMessage("hello", "text-only-model", []string{"/tmp/x.png"}, nil)
	if err != nil {
		t.Fatalf("buildLineCurrentMessage() error = %v", err)
	}
	if len(msg.Parts) != 0 {
		t.Fatalf("parts len = %d, want 0", len(msg.Parts))
	}
}

func TestBuildLinePromptMessagesSeparatesHistoryAndCurrent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	if err := os.WriteFile(path, tinyPNG, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	historyMsg, currentMsg, err := buildLinePromptMessages([]chathistory.ChatHistoryItem{{
		Channel:   chathistory.ChannelLine,
		Kind:      chathistory.KindInboundUser,
		MessageID: "101",
		SentAt:    time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC),
		Text:      "earlier",
	}}, lineJob{
		ChatID:       "C123",
		ChatType:     "group",
		MessageID:    "102",
		ReplyToken:   "rtok_1",
		FromUserID:   "U1",
		FromUsername: "alice",
		DisplayName:  "Alice",
		Text:         "latest",
		ImagePaths:   []string{path},
		SentAt:       time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
	}, "gpt-5.2", true, nil)
	if err != nil {
		t.Fatalf("buildLinePromptMessages() error = %v", err)
	}
	if historyMsg == nil {
		t.Fatalf("historyMsg = nil")
	}
	if strings.Contains(historyMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("history should not contain latest message: %s", historyMsg.Content)
	}
	if !strings.Contains(historyMsg.Content, "\"text\": \"earlier\"") {
		t.Fatalf("history should contain prior message: %s", historyMsg.Content)
	}
	if currentMsg == nil {
		t.Fatalf("currentMsg = nil")
	}
	if !strings.Contains(currentMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("current message should contain latest text: %s", currentMsg.Content)
	}
	if len(historyMsg.Parts) != 0 {
		t.Fatalf("history parts len = %d, want 0", len(historyMsg.Parts))
	}
	if len(currentMsg.Parts) != 2 {
		t.Fatalf("current parts len = %d, want 2", len(currentMsg.Parts))
	}
}

func TestBuildLinePromptMessagesOmitsEmptyHistory(t *testing.T) {
	t.Parallel()

	historyMsg, currentMsg, err := buildLinePromptMessages(nil, lineJob{
		ChatID:       "C123",
		ChatType:     "group",
		MessageID:    "102",
		ReplyToken:   "rtok_1",
		FromUserID:   "U1",
		FromUsername: "alice",
		DisplayName:  "Alice",
		Text:         "latest",
		SentAt:       time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
	}, "gpt-5.2", false, nil)
	if err != nil {
		t.Fatalf("buildLinePromptMessages() error = %v", err)
	}
	if historyMsg != nil {
		t.Fatalf("historyMsg should be nil when history is empty")
	}
	if currentMsg == nil || !strings.Contains(currentMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("current message should still be present: %#v", currentMsg)
	}
}

func TestBuildLineCurrentMessageImageTooLarge(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "large.png")
	large := make([]byte, lineLLMMaxImageBytes+1)
	if err := os.WriteFile(path, large, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	_, err := buildLineCurrentMessage("hello", "gpt-5.2", []string{path}, nil)
	if err == nil {
		t.Fatalf("buildLineCurrentMessage() expected error")
	}
	if !strings.Contains(err.Error(), "图片太大") {
		t.Fatalf("error = %v, want 图片太大", err)
	}
}

func TestBuildLineCurrentMessageSkipsUnknownFileTypes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "image.bin")
	if err := os.WriteFile(path, []byte("not-an-image"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	msg, err := buildLineCurrentMessage("hello", "gpt-5.2", []string{path}, nil)
	if err != nil {
		t.Fatalf("buildLineCurrentMessage() error = %v", err)
	}
	if len(msg.Parts) != 0 {
		t.Fatalf("parts len = %d, want 0", len(msg.Parts))
	}
	if msg.Content != "hello" {
		t.Fatalf("content = %q, want hello", msg.Content)
	}
}

func TestDownloadLineImageToCache(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/bot/message/m_1001/content" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/v2/bot/message/m_1001/content")
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyPNG)
	}))
	defer srv.Close()

	api := newLineAPI(srv.Client(), srv.URL, "line-token")
	dir := t.TempDir()
	path, err := downloadLineImageToCache(context.Background(), api, dir, "m_1001", 1024*1024)
	if err != nil {
		t.Fatalf("downloadLineImageToCache() error = %v", err)
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("downloaded extension = %q, want .png", filepath.Ext(path))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(raw) != string(tinyPNG) {
		t.Fatalf("downloaded content mismatch")
	}
}

func TestDownloadLineImageToCacheUnsupportedMime(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		_, _ = io.WriteString(w, "gif-data")
	}))
	defer srv.Close()

	api := newLineAPI(srv.Client(), srv.URL, "line-token")
	_, err := downloadLineImageToCache(context.Background(), api, t.TempDir(), "m_1002", 1024*1024)
	if err == nil {
		t.Fatalf("downloadLineImageToCache() expected error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v, want unsupported error", err)
	}
}
