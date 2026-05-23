package telegram

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestSendPhotoUsesSendPhotoEndpointAndMultipartPayload(t *testing.T) {
	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "photo.png")
	if err := os.WriteFile(imagePath, []byte("fake-image"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sawChatID := ""
	sawMessageThreadID := ""
	sawCaption := ""
	sawFileField := ""
	sawFilename := ""
	sawFileBody := ""
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendPhoto" {
			http.NotFound(w, r)
			return
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if mediaType != "multipart/form-data" {
			http.Error(w, "unexpected content type", http.StatusBadRequest)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			body, err := io.ReadAll(part)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			switch part.FormName() {
			case "chat_id":
				sawChatID = string(body)
			case "message_thread_id":
				sawMessageThreadID = string(body)
			case "caption":
				sawCaption = string(body)
			case "photo":
				sawFileField = part.FormName()
				sawFilename = part.FileName()
				sawFileBody = string(body)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	api := newTelegramAPI(srv.Client, srv.URL, "token")
	if err := api.sendPhotoInThread(context.Background(), 42, 901, imagePath, "renamed.png", "caption"); err != nil {
		t.Fatalf("sendPhotoInThread() error = %v", err)
	}
	if sawChatID != "42" {
		t.Fatalf("chat_id = %q, want 42", sawChatID)
	}
	if sawMessageThreadID != "901" {
		t.Fatalf("message_thread_id = %q, want 901", sawMessageThreadID)
	}
	if sawCaption != "caption" {
		t.Fatalf("caption = %q, want caption", sawCaption)
	}
	if sawFileField != "photo" {
		t.Fatalf("file field = %q, want photo", sawFileField)
	}
	if sawFilename != "renamed.png" {
		t.Fatalf("filename = %q, want renamed.png", sawFilename)
	}
	if sawFileBody != "fake-image" {
		t.Fatalf("file body = %q, want fake-image", sawFileBody)
	}
}
