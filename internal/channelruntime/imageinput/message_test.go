package imageinput

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

func TestBuildUserMessageWithImageParts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	raw := []byte("png-data")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	msg, err := BuildUserMessage("hello", "gpt-5.2", []string{path}, MessageOptions{
		MaxImages: 3,
		MaxBytes:  1024,
	})
	if err != nil {
		t.Fatalf("BuildUserMessage() error = %v", err)
	}
	if len(msg.Parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(msg.Parts))
	}
	if msg.Parts[0].Type != llm.PartTypeText || msg.Parts[0].Text != "hello" {
		t.Fatalf("text part mismatch: %+v", msg.Parts[0])
	}
	if msg.Parts[1].Type != llm.PartTypeImageBase64 {
		t.Fatalf("image part type = %q, want %q", msg.Parts[1].Type, llm.PartTypeImageBase64)
	}
	if msg.Parts[1].MIMEType != "image/png" {
		t.Fatalf("image MIME = %q, want image/png", msg.Parts[1].MIMEType)
	}
	if msg.Parts[1].DataBase64 != base64.StdEncoding.EncodeToString(raw) {
		t.Fatalf("image data mismatch")
	}
}

func TestBuildUserMessageSkipsUnknownTypes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "image.bin")
	if err := os.WriteFile(path, []byte("not-image"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	msg, err := BuildUserMessage("hello", "gpt-5.2", []string{path}, MessageOptions{
		MaxImages: 3,
		MaxBytes:  1024,
	})
	if err != nil {
		t.Fatalf("BuildUserMessage() error = %v", err)
	}
	if len(msg.Parts) != 0 {
		t.Fatalf("parts len = %d, want 0", len(msg.Parts))
	}
	if msg.Content != "hello" {
		t.Fatalf("content = %q, want hello", msg.Content)
	}
}

func TestBuildUserMessageTranscode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "image.jpg")
	if err := os.WriteFile(path, []byte("jpg-data"), 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	msg, err := BuildUserMessage("hello", "gpt-5.2", []string{path}, MessageOptions{
		MaxImages: 3,
		MaxBytes:  1024,
		Transcode: func(raw []byte, mimeType string) ([]byte, string, error) {
			if mimeType != "image/jpeg" {
				t.Fatalf("transcode MIME = %q, want image/jpeg", mimeType)
			}
			return []byte("webp-data"), "image/webp", nil
		},
	})
	if err != nil {
		t.Fatalf("BuildUserMessage() error = %v", err)
	}
	if len(msg.Parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(msg.Parts))
	}
	if msg.Parts[1].MIMEType != "image/webp" {
		t.Fatalf("image MIME = %q, want image/webp", msg.Parts[1].MIMEType)
	}
	if msg.Parts[1].DataBase64 != base64.StdEncoding.EncodeToString([]byte("webp-data")) {
		t.Fatalf("image data mismatch")
	}
}
