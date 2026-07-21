package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/filecache"
)

// API is the minimal Telegram transport surface needed by telegram tools.
type API interface {
	SendDocument(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error
	SendPhoto(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error
	SendVoice(ctx context.Context, chatID int64, messageThreadID int64, filePath string, filename string, caption string) error
	SetEmojiReaction(ctx context.Context, chatID int64, messageID int64, emoji string, isBig *bool) error
}

type Reaction struct {
	ChatID    int64
	MessageID int64
	Emoji     string
	Source    string
}

type SendFileTool struct {
	api      API
	chatID   int64
	threadID int64
	cacheDir string
	maxBytes int64
}

func NewSendFileTool(api API, chatID int64, messageThreadID int64, cacheDir string, maxBytes int64) *SendFileTool {
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}
	return &SendFileTool{
		api:      api,
		chatID:   chatID,
		threadID: messageThreadID,
		cacheDir: strings.TrimSpace(cacheDir),
		maxBytes: maxBytes,
	}
}

func (t *SendFileTool) Name() string { return "telegram_send_file" }

func (t *SendFileTool) Description() string {
	return "Sends a local file (from file_cache_dir) back to the current chat as a document. If you need more advanced behavior, describe it in text instead."
}

func (t *SendFileTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to a local file under file_cache_dir (absolute or relative to that directory).",
			},
			"filename": map[string]any{
				"type":        "string",
				"description": "Optional filename shown to the user (default: basename of path).",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional caption text.",
			},
		},
		"required": []string{"path"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *SendFileTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.api == nil {
		return "", fmt.Errorf("telegram_send_file is disabled")
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

	filename, _ := params["filename"].(string)
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = pathAbs
	}
	filename = filecache.SanitizeFilename(filename)

	caption, _ := params["caption"].(string)
	caption = strings.TrimSpace(caption)

	if err := t.api.SendDocument(ctx, t.chatID, t.threadID, pathAbs, filename, caption); err != nil {
		return "", err
	}
	return fmt.Sprintf("sent file: %s", filename), nil
}
