package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
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
