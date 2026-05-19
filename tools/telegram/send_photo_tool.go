package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type SendPhotoTool struct {
	api      API
	chatID   int64
	threadID int64
	cacheDir string
	maxBytes int64
}

func NewSendPhotoTool(api API, chatID int64, messageThreadID int64, cacheDir string, maxBytes int64) *SendPhotoTool {
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}
	return &SendPhotoTool{
		api:      api,
		chatID:   chatID,
		threadID: messageThreadID,
		cacheDir: strings.TrimSpace(cacheDir),
		maxBytes: maxBytes,
	}
}

func (t *SendPhotoTool) Name() string { return "telegram_send_photo" }

func (t *SendPhotoTool) Description() string {
	return "Sends a local image (from file_cache_dir) back to the current chat as an inline Telegram photo. Use telegram_send_file instead when you want it delivered as a document."
}

func (t *SendPhotoTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to a local image file under file_cache_dir (absolute or relative to that directory).",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional photo caption text.",
			},
		},
		"required": []string{"path"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *SendPhotoTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.api == nil {
		return "", fmt.Errorf("telegram_send_photo is disabled")
	}
	rawPath, _ := params["path"].(string)
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("missing required param: path")
	}
	cacheDir := strings.TrimSpace(t.cacheDir)
	if cacheDir == "" {
		return "", fmt.Errorf("file cache dir is not configured")
	}
	pathAbs, err := resolveFileCachePath(cacheDir, rawPath, t.maxBytes)
	if err != nil {
		return "", err
	}

	caption, _ := params["caption"].(string)
	caption = strings.TrimSpace(caption)

	filename := sanitizeFilename(filepath.Base(pathAbs))
	if err := t.api.SendPhoto(ctx, t.chatID, t.threadID, pathAbs, filename, caption); err != nil {
		return "", err
	}
	return fmt.Sprintf("sent photo: %s", filename), nil
}
