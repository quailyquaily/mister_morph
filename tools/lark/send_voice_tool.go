package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type SendVoiceTool struct {
	api      API
	chatID   string
	cacheDir string
	maxBytes int64
}

func NewSendVoiceTool(api API, chatID string, cacheDir string, maxBytes int64) *SendVoiceTool {
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}
	return &SendVoiceTool{
		api:      api,
		chatID:   strings.TrimSpace(chatID),
		cacheDir: strings.TrimSpace(cacheDir),
		maxBytes: maxBytes,
	}
}

func (t *SendVoiceTool) Name() string { return "lark_send_voice" }

func (t *SendVoiceTool) Description() string {
	return "Sends a local OPUS audio file from file_cache_dir to the current Lark chat as an audio message."
}

func (t *SendVoiceTool) ParameterSchema() string {
	s := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to a local OPUS audio file under file_cache_dir, absolute or relative to that directory.",
			},
			"filename": map[string]any{
				"type":        "string",
				"description": "Optional filename shown to the user. Defaults to the file basename.",
			},
		},
		"required": []string{"path"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *SendVoiceTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.api == nil {
		return "", fmt.Errorf("lark_send_voice is disabled")
	}
	if strings.TrimSpace(t.chatID) == "" {
		return "", fmt.Errorf("lark chat id is not configured")
	}
	cacheDir := strings.TrimSpace(t.cacheDir)
	if cacheDir == "" {
		return "", fmt.Errorf("file cache dir is not configured")
	}
	rawPath, _ := params["path"].(string)
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("missing required param: path")
	}
	pathAbs, err := resolveFileCachePath(cacheDir, rawPath, t.maxBytes)
	if err != nil {
		return "", err
	}
	filename, _ := params["filename"].(string)
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = filepath.Base(pathAbs)
	}
	filename = sanitizeFilename(filename)

	if err := t.api.SendVoice(ctx, t.chatID, pathAbs, filename); err != nil {
		return "", err
	}
	return fmt.Sprintf("sent voice: %s", filename), nil
}
