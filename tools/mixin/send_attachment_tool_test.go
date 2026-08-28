package mixin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

type fakeAttachmentAPI struct {
	uploaded []byte
	mimeType string
	messages []mixinapi.MessageRequest
}

func (f *fakeAttachmentAPI) CreateAttachment(context.Context) (mixinapi.Attachment, error) {
	return mixinapi.Attachment{
		AttachmentID: "11111111-1111-1111-1111-111111111111",
		UploadURL:    "https://example.invalid/upload",
	}, nil
}

func (f *fakeAttachmentAPI) UploadAttachment(_ context.Context, _ mixinapi.Attachment, contentType string, _ int64, source io.Reader) error {
	f.uploaded, _ = io.ReadAll(source)
	f.mimeType = contentType
	return nil
}

func (f *fakeAttachmentAPI) SendMessages(_ context.Context, messages []mixinapi.MessageRequest) error {
	f.messages = append(f.messages, messages...)
	return nil
}

func TestSendAttachmentToolUploadsAndSendsCategoryPayload(t *testing.T) {
	cacheDir := t.TempDir()
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		kind         AttachmentKind
		filename     string
		content      []byte
		wantName     string
		wantCategory string
		wantMIME     string
	}{
		{kind: AttachmentFile, filename: "report.txt", content: []byte("report"), wantName: "mixin_send_file", wantCategory: mixinapi.MessageCategoryPlainData, wantMIME: "text/plain"},
		{kind: AttachmentPhoto, filename: "photo.png", content: imageData.Bytes(), wantName: "mixin_send_photo", wantCategory: mixinapi.MessageCategoryPlainImage, wantMIME: "image/png"},
		{kind: AttachmentAudio, filename: "voice.ogg", content: []byte("OggS-audio"), wantName: "mixin_send_audio", wantCategory: mixinapi.MessageCategoryPlainAudio, wantMIME: "audio/ogg"},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			path := filepath.Join(cacheDir, tt.filename)
			if err := os.WriteFile(path, tt.content, 0o600); err != nil {
				t.Fatal(err)
			}
			api := &fakeAttachmentAPI{}
			tool := NewSendAttachmentTool(api, "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333", cacheDir, 1024*1024, tt.kind)
			if tool.Name() != tt.wantName {
				t.Fatalf("Name() = %q", tool.Name())
			}
			if _, err := tool.Execute(context.Background(), map[string]any{"path": tt.filename, "caption": "sent"}); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !bytes.Equal(api.uploaded, tt.content) || api.mimeType != tt.wantMIME {
				t.Fatalf("upload = %q, %q", api.uploaded, api.mimeType)
			}
			if len(api.messages) != 2 || api.messages[0].Category != tt.wantCategory || api.messages[1].Category != mixinapi.MessageCategoryPlainText {
				t.Fatalf("messages = %#v", api.messages)
			}
			if api.messages[0].RecipientID != "33333333-3333-3333-3333-333333333333" || api.messages[1].RecipientID != "33333333-3333-3333-3333-333333333333" {
				t.Fatalf("message recipients = %#v", api.messages)
			}
			raw, err := base64.RawURLEncoding.DecodeString(api.messages[0].DataBase64)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["attachment_id"] != "11111111-1111-1111-1111-111111111111" || payload["size"] != float64(len(tt.content)) {
				t.Fatalf("payload = %#v", payload)
			}
			if tt.kind == AttachmentPhoto && (payload["width"] != float64(2) || payload["height"] != float64(3)) {
				t.Fatalf("image payload = %#v", payload)
			}
		})
	}
}

func TestSendPhotoToolRejectsNonImage(t *testing.T) {
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cacheDir, "not-image.png"), []byte("plain text"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := NewSendAttachmentTool(&fakeAttachmentAPI{}, "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333", cacheDir, 1024, AttachmentPhoto)
	if _, err := tool.Execute(context.Background(), map[string]any{"path": "not-image.png"}); err == nil {
		t.Fatal("Execute() error = nil")
	}
}
