package mixin

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/internal/filecache"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

type AttachmentKind string

const (
	AttachmentFile  AttachmentKind = "file"
	AttachmentPhoto AttachmentKind = "photo"
	AttachmentAudio AttachmentKind = "audio"
)

type AttachmentAPI interface {
	CreateAttachment(context.Context) (mixinapi.Attachment, error)
	UploadAttachment(context.Context, mixinapi.Attachment, string, int64, io.Reader) error
	SendMessages(context.Context, []mixinapi.MessageRequest) error
}

type SendAttachmentTool struct {
	api            AttachmentAPI
	conversationID string
	recipientID    string
	cacheDir       string
	maxBytes       int64
	kind           AttachmentKind
}

func NewSendAttachmentTool(api AttachmentAPI, conversationID, recipientID, cacheDir string, maxBytes int64, kind AttachmentKind) *SendAttachmentTool {
	if maxBytes <= 0 {
		maxBytes = 20 << 20
	}
	return &SendAttachmentTool{
		api: api, conversationID: strings.TrimSpace(conversationID), recipientID: strings.TrimSpace(recipientID), cacheDir: strings.TrimSpace(cacheDir),
		maxBytes: maxBytes, kind: kind,
	}
}

func (t *SendAttachmentTool) Name() string {
	switch t.kind {
	case AttachmentPhoto:
		return "mixin_send_photo"
	case AttachmentAudio:
		return "mixin_send_audio"
	default:
		return "mixin_send_file"
	}
}

func (t *SendAttachmentTool) Description() string {
	switch t.kind {
	case AttachmentPhoto:
		return "Sends a local image from file_cache_dir to the current Mixin conversation as a photo."
	case AttachmentAudio:
		return "Sends a local audio file from file_cache_dir to the current Mixin conversation as audio."
	default:
		return "Sends a local file from file_cache_dir to the current Mixin conversation."
	}
}

func (t *SendAttachmentTool) ParameterSchema() string {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path":     map[string]any{"type": "string", "description": "Path to a local file under file_cache_dir, absolute or relative to that directory."},
			"filename": map[string]any{"type": "string", "description": "Optional filename shown to the user. Defaults to the file basename."},
			"caption":  map[string]any{"type": "string", "description": "Optional caption sent as a text message after the attachment."},
		},
		"required": []string{"path"},
	}
	raw, _ := json.MarshalIndent(schema, "", "  ")
	return string(raw)
}

func (t *SendAttachmentTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.api == nil {
		return "", fmt.Errorf("mixin attachment sending is disabled")
	}
	conversationID, err := uuid.Parse(t.conversationID)
	if err != nil || conversationID == uuid.Nil {
		return "", fmt.Errorf("mixin conversation id is invalid")
	}
	rawPath, _ := params["path"].(string)
	path, err := filecache.ResolveFile(t.cacheDir, strings.TrimSpace(rawPath), t.maxBytes)
	if err != nil {
		return "", err
	}
	filename, _ := params["filename"].(string)
	if filename = strings.TrimSpace(filename); filename == "" {
		filename = filepath.Base(path)
	}
	filename = filecache.SanitizeFilename(filename)
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	mimeType, imageWidth, imageHeight, err := mixinAttachmentMetadata(file, path, t.kind)
	if err != nil {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	attachment, err := t.api.CreateAttachment(ctx)
	if err != nil {
		return "", err
	}
	if err := t.api.UploadAttachment(ctx, attachment, mimeType, info.Size(), file); err != nil {
		return "", err
	}
	payload := map[string]any{
		"attachment_id": strings.TrimSpace(attachment.AttachmentID),
		"mime_type":     mimeType,
		"size":          info.Size(),
	}
	category := mixinapi.MessageCategoryPlainData
	switch t.kind {
	case AttachmentPhoto:
		category = mixinapi.MessageCategoryPlainImage
		payload["width"] = imageWidth
		payload["height"] = imageHeight
	case AttachmentAudio:
		category = mixinapi.MessageCategoryPlainAudio
		payload["waveform"] = ""
		payload["duration"] = 0
		payload["created_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	default:
		payload["name"] = filename
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	messageID := uuid.New()
	messages := []mixinapi.MessageRequest{{
		ConversationID: conversationID.String(), RecipientID: t.recipientID, MessageID: messageID.String(), Category: category,
		DataBase64: base64.RawURLEncoding.EncodeToString(rawPayload),
	}}
	caption, _ := params["caption"].(string)
	if caption = strings.TrimSpace(caption); caption != "" {
		captionID := uuid.NewSHA1(messageID, []byte("caption"))
		messages = append(messages, mixinapi.MessageRequest{
			ConversationID: conversationID.String(), RecipientID: t.recipientID, MessageID: captionID.String(), Category: mixinapi.MessageCategoryPlainText,
			DataBase64: base64.RawURLEncoding.EncodeToString([]byte(caption)), QuoteMessageID: messageID.String(),
		})
	}
	if err := t.api.SendMessages(ctx, messages); err != nil {
		return "", err
	}
	return fmt.Sprintf("sent %s: %s", t.kind, filename), nil
}

func mixinAttachmentMetadata(file *os.File, path string, kind AttachmentKind) (string, int, int, error) {
	reader := bufio.NewReader(file)
	header, err := reader.Peek(512)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return "", 0, 0, err
	}
	detected := strings.ToLower(strings.TrimSpace(http.DetectContentType(header)))
	byExtension := strings.ToLower(strings.TrimSpace(mime.TypeByExtension(filepath.Ext(path))))
	if parsed, _, parseErr := mime.ParseMediaType(byExtension); parseErr == nil {
		byExtension = parsed
	}
	mimeType := detected
	if kind != AttachmentPhoto && byExtension != "" && byExtension != "application/octet-stream" {
		mimeType = byExtension
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, 0, err
	}
	switch kind {
	case AttachmentPhoto:
		if !strings.HasPrefix(detected, "image/") {
			return "", 0, 0, fmt.Errorf("mixin_send_photo requires an image file")
		}
		config, _, err := image.DecodeConfig(file)
		if err != nil {
			return "", 0, 0, fmt.Errorf("read image dimensions: %w", err)
		}
		return detected, config.Width, config.Height, nil
	case AttachmentAudio:
		if !strings.HasPrefix(mimeType, "audio/") {
			return "", 0, 0, fmt.Errorf("mixin_send_audio requires an audio file")
		}
	}
	return mimeType, 0, 0, nil
}
