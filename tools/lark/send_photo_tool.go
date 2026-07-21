package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/filecache"
)

type SendPhotoTool struct {
	api      API
	chatID   string
	cacheDir string
	maxBytes int64
}

func NewSendPhotoTool(api API, chatID string, cacheDir string, maxBytes int64) *SendPhotoTool {
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}
	return &SendPhotoTool{
		api:      api,
		chatID:   strings.TrimSpace(chatID),
		cacheDir: strings.TrimSpace(cacheDir),
		maxBytes: maxBytes,
	}
}

func (t *SendPhotoTool) Name() string { return "lark_send_photo" }

func (t *SendPhotoTool) Description() string {
	return "Sends a local image from file_cache_dir to the current Lark chat as an image message. Use lark_send_file when it should be delivered as a file."
}

func (t *SendPhotoTool) ParameterSchema() string {
	s := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to a local image under file_cache_dir, absolute or relative to that directory.",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional caption text. Lark sends it as a separate text message after the image.",
			},
		},
		"required": []string{"path"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *SendPhotoTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.api == nil {
		return "", fmt.Errorf("lark_send_photo is disabled")
	}
	if strings.TrimSpace(t.chatID) == "" {
		return "", fmt.Errorf("lark chat id is not configured")
	}
	rawPath, _ := params["path"].(string)
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("missing required param: path")
	}
	pathAbs, err := filecache.ResolveFile(t.cacheDir, rawPath, t.maxBytes)
	if err != nil {
		return "", err
	}
	caption, _ := params["caption"].(string)
	caption = strings.TrimSpace(caption)
	filename := filecache.SanitizeFilename(pathAbs)
	if err := t.api.SendPhoto(ctx, t.chatID, pathAbs, filename, caption); err != nil {
		return "", err
	}
	return fmt.Sprintf("sent photo: %s", filename), nil
}
