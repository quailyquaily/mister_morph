package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// API is the Lark transport surface used by Lark runtime tools.
type API interface {
	SendFile(ctx context.Context, chatID string, filePath string, filename string, caption string) error
	SendPhoto(ctx context.Context, chatID string, filePath string, filename string, caption string) error
	SendVoice(ctx context.Context, chatID string, filePath string, filename string) error
	SetEmojiReaction(ctx context.Context, messageID string, emojiType string) error
}

type SendFileTool struct {
	api      API
	chatID   string
	cacheDir string
	maxBytes int64
}

func NewSendFileTool(api API, chatID string, cacheDir string, maxBytes int64) *SendFileTool {
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}
	return &SendFileTool{
		api:      api,
		chatID:   strings.TrimSpace(chatID),
		cacheDir: strings.TrimSpace(cacheDir),
		maxBytes: maxBytes,
	}
}

func (t *SendFileTool) Name() string { return "lark_send_file" }

func (t *SendFileTool) Description() string {
	return "Sends a local file from file_cache_dir to the current Lark chat as a file message."
}

func (t *SendFileTool) ParameterSchema() string {
	s := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to a local file under file_cache_dir, absolute or relative to that directory.",
			},
			"filename": map[string]any{
				"type":        "string",
				"description": "Optional filename shown to the user. Defaults to the file basename.",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional caption text. Lark sends it as a separate text message after the file.",
			},
		},
		"required": []string{"path"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *SendFileTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.api == nil {
		return "", fmt.Errorf("lark_send_file is disabled")
	}
	if strings.TrimSpace(t.chatID) == "" {
		return "", fmt.Errorf("lark chat id is not configured")
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

	filename, _ := params["filename"].(string)
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = filepath.Base(pathAbs)
	}
	filename = sanitizeFilename(filename)

	caption, _ := params["caption"].(string)
	caption = strings.TrimSpace(caption)

	if err := t.api.SendFile(ctx, t.chatID, pathAbs, filename, caption); err != nil {
		return "", err
	}
	return fmt.Sprintf("sent file: %s", filename), nil
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "file"
	}
	name = filepath.Base(name)
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-' || r == '+':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._- ")
	if out == "" {
		return "file"
	}
	const max = 120
	if len(out) > max {
		out = out[:max]
	}
	return out
}
