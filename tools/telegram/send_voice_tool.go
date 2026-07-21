package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/filecache"
)

type SendVoiceTool struct {
	api        API
	defaultTo  int64
	threadID   int64
	cacheDir   string
	maxBytes   int64
	allowedIDs map[int64]bool
}

func NewSendVoiceTool(api API, defaultChatID int64, messageThreadID int64, cacheDir string, maxBytes int64, allowedIDs map[int64]bool) *SendVoiceTool {
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}
	return &SendVoiceTool{
		api:        api,
		defaultTo:  defaultChatID,
		threadID:   messageThreadID,
		cacheDir:   strings.TrimSpace(cacheDir),
		maxBytes:   maxBytes,
		allowedIDs: allowedIDs,
	}
}

func (t *SendVoiceTool) Name() string { return "telegram_send_voice" }

func (t *SendVoiceTool) Description() string {
	return "Sends a Telegram voice message from a local audio file under `file_cache_dir`. Use chat_id when not running in an active chat context." +
		"It only sends an existing local voice file. If you do not have a file path, try to generate one."
}

func (t *SendVoiceTool) ParameterSchema() string {
	s := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"chat_id": map[string]any{
				"type":        "integer",
				"description": "Target Telegram chat_id. Optional in interactive chat context; required for scheduled runs unless default chat_id is set.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Path to a local voice file under file_cache_dir (absolute or relative to that directory). Recommended: .ogg or .mp3 file.",
			},
			"filename": map[string]any{
				"type":        "string",
				"description": "Optional filename shown to the user (default: basename of path).",
			},
		},
		"required": []string{"path"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *SendVoiceTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.api == nil {
		return "", fmt.Errorf("telegram_send_voice is disabled")
	}

	chatID := t.defaultTo
	if v, ok := params["chat_id"]; ok {
		switch x := v.(type) {
		case int64:
			chatID = x
		case int:
			chatID = int64(x)
		case float64:
			chatID = int64(x)
		}
	}
	if chatID == 0 {
		return "", fmt.Errorf("missing required param: chat_id")
	}
	if len(t.allowedIDs) > 0 && !t.allowedIDs[chatID] {
		return "", fmt.Errorf("unauthorized chat_id: %d", chatID)
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

	// Voice captions are intentionally not supported by telegram_send_voice.
	messageThreadID := int64(0)
	if chatID == t.defaultTo {
		messageThreadID = t.threadID
	}
	if err := t.api.SendVoice(ctx, chatID, messageThreadID, pathAbs, filename, ""); err != nil {
		return "", err
	}
	return fmt.Sprintf("sent voice: %s", filename), nil
}
