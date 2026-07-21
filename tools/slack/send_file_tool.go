package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/filecache"
)

type SendFileTool struct {
	api               API
	defaultChannelID  string
	defaultThreadTS   string
	allowedChannelIDs map[string]bool
	cacheDir          string
	maxBytes          int64
}

func NewSendFileTool(api API, defaultChannelID, defaultThreadTS string, allowedChannelIDs map[string]bool, cacheDir string, maxBytes int64) *SendFileTool {
	if maxBytes <= 0 {
		maxBytes = 20 * 1024 * 1024
	}
	allowed := make(map[string]bool, len(allowedChannelIDs))
	for raw := range allowedChannelIDs {
		channelID := strings.TrimSpace(raw)
		if channelID == "" {
			continue
		}
		allowed[channelID] = true
	}
	return &SendFileTool{
		api:               api,
		defaultChannelID:  strings.TrimSpace(defaultChannelID),
		defaultThreadTS:   strings.TrimSpace(defaultThreadTS),
		allowedChannelIDs: allowed,
		cacheDir:          strings.TrimSpace(cacheDir),
		maxBytes:          maxBytes,
	}
}

func (t *SendFileTool) Name() string { return "slack_send_file" }

func (t *SendFileTool) Description() string {
	return "Uploads a local file under file_cache_dir to Slack. Use this when you need to send generated artifacts back to the current channel."
}

func (t *SendFileTool) ParameterSchema() string {
	s := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"channel_id": map[string]any{
				"type":        "string",
				"description": "Target Slack channel id. Optional in active channel context.",
			},
			"thread_ts": map[string]any{
				"type":        "string",
				"description": "Optional thread timestamp to keep upload in the same thread.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Path to a local file under file_cache_dir (absolute or relative to that directory).",
			},
			"filename": map[string]any{
				"type":        "string",
				"description": "Optional display filename (default: basename of path).",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Optional title shown in Slack.",
			},
			"initial_comment": map[string]any{
				"type":        "string",
				"description": "Optional message text attached to the file upload.",
			},
		},
		"required": []string{"path"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *SendFileTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.api == nil {
		return "", fmt.Errorf("slack_send_file is disabled")
	}

	channelID := strings.TrimSpace(t.defaultChannelID)
	if v, ok := params["channel_id"].(string); ok {
		channelID = strings.TrimSpace(v)
	}
	if channelID == "" {
		return "", fmt.Errorf("missing required param: channel_id")
	}
	if len(t.allowedChannelIDs) > 0 && !t.allowedChannelIDs[channelID] {
		return "", fmt.Errorf("unauthorized channel_id: %s", channelID)
	}

	threadTS := strings.TrimSpace(t.defaultThreadTS)
	if v, ok := params["thread_ts"].(string); ok {
		threadTS = strings.TrimSpace(v)
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

	title, _ := params["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = filename
	}

	initialComment, _ := params["initial_comment"].(string)
	initialComment = strings.TrimSpace(initialComment)

	if err := t.api.SendFile(ctx, channelID, threadTS, pathAbs, filename, title, initialComment); err != nil {
		return "", err
	}
	return fmt.Sprintf("uploaded file: %s", filename), nil
}
