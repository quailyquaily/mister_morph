package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/memory"
)

type MemoryRecentlyTool struct {
	Enabled     bool
	MemoryDir   string
	DefaultDays int
	MaxItems    int
}

func NewMemoryRecentlyTool(enabled bool, memoryDir string, defaultDays int, maxItems int) *MemoryRecentlyTool {
	if defaultDays <= 0 {
		defaultDays = 3
	}
	if maxItems <= 0 {
		maxItems = 50
	}
	return &MemoryRecentlyTool{
		Enabled:     enabled,
		MemoryDir:   strings.TrimSpace(memoryDir),
		DefaultDays: defaultDays,
		MaxItems:    maxItems,
	}
}

func (t *MemoryRecentlyTool) Name() string { return "memory_recently" }

func (t *MemoryRecentlyTool) Description() string {
	return "Reads recent short-term memory items. Returns summary plus metadata (including contact_id and Telegram chat hints when available)."
}

func (t *MemoryRecentlyTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"days": map[string]any{
				"type":        "integer",
				"description": "Recent day window (default 3).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max records to return.",
			},
			"include_body": map[string]any{
				"type":        "boolean",
				"description": "Include parsed short-term body fields for each item.",
			},
		},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

type recentMemoryItem struct {
	Date             string                   `json:"date"`
	Summary          string                   `json:"summary"`
	RelPath          string                   `json:"rel_path"`
	CreatedAt        string                   `json:"created_at,omitempty"`
	UpdatedAt        string                   `json:"updated_at,omitempty"`
	SessionID        string                   `json:"session_id,omitempty"`
	Source           string                   `json:"source,omitempty"`
	Channel          string                   `json:"channel,omitempty"`
	SubjectID        string                   `json:"subject_id,omitempty"`
	ContactID        string                   `json:"contact_id,omitempty"`
	ContactNickname  string                   `json:"contact_nickname,omitempty"`
	Usernames        []string                 `json:"usernames,omitempty"`
	TelegramChatID   int64                    `json:"telegram_chat_id,omitempty"`
	TelegramChatType string                   `json:"telegram_chat_type,omitempty"`
	TasksDone        int                      `json:"tasks_done,omitempty"`
	TasksTotal       int                      `json:"tasks_total,omitempty"`
	FollowUpsDone    int                      `json:"follow_ups_done,omitempty"`
	FollowUpsTotal   int                      `json:"follow_ups_total,omitempty"`
	Body             *memory.ShortTermContent `json:"body,omitempty"`
}

func (t *MemoryRecentlyTool) Execute(_ context.Context, params map[string]any) (string, error) {
	if !t.Enabled {
		return "", fmt.Errorf("memory_recently tool is disabled")
	}
	dir := pathutil.ExpandHomePath(strings.TrimSpace(t.MemoryDir))
	if dir == "" {
		return "", fmt.Errorf("memory dir is not configured")
	}
	days := parseIntDefault(params["days"], t.DefaultDays)
	if days <= 0 {
		days = t.DefaultDays
	}
	limit := parseIntDefault(params["limit"], t.MaxItems)
	if limit <= 0 || limit > t.MaxItems {
		limit = t.MaxItems
	}
	includeBody := parseBoolDefault(params["include_body"], false)

	mgr := memory.NewManager(dir, days)
	summaries, err := mgr.LoadShortTermSummaries(days)
	if err != nil {
		return "", err
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].Date == summaries[j].Date {
			return summaries[i].RelPath > summaries[j].RelPath
		}
		return summaries[i].Date > summaries[j].Date
	})

	items := make([]recentMemoryItem, 0, len(summaries))
	for _, summary := range summaries {
		if len(items) >= limit {
			break
		}
		item := recentMemoryItem{
			Date:           summary.Date,
			Summary:        strings.TrimSpace(summary.Summary),
			RelPath:        strings.TrimSpace(summary.RelPath),
			TasksDone:      summary.TasksDone,
			TasksTotal:     summary.TasksTotal,
			FollowUpsDone:  summary.FollowUpsDone,
			FollowUpsTotal: summary.FollowUpsTotal,
		}
		abs := filepath.Join(dir, filepath.FromSlash(summary.RelPath))
		raw, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		fm, body, ok := memory.ParseFrontmatter(string(raw))
		if ok {
			item.CreatedAt = strings.TrimSpace(fm.CreatedAt)
			item.UpdatedAt = strings.TrimSpace(fm.UpdatedAt)
			item.SessionID = strings.TrimSpace(fm.SessionID)
			item.Source = strings.TrimSpace(fm.Source)
			item.Channel = strings.TrimSpace(fm.Channel)
			item.SubjectID = strings.TrimSpace(fm.SubjectID)
			item.ContactID = strings.TrimSpace(fm.ContactID)
			item.ContactNickname = strings.TrimSpace(fm.ContactNickname)
			item.Usernames = dedupeStrings(fm.Usernames)
		}
		if chatID, ok := parseTelegramSessionID(item.SessionID); ok {
			item.TelegramChatID = chatID
			item.TelegramChatType = strings.ToLower(strings.TrimSpace(item.Channel))
		}
		if includeBody {
			content := memory.ParseShortTermContent(body)
			item.Body = &content
		}
		items = append(items, item)
	}

	out, _ := json.MarshalIndent(map[string]any{
		"days":         days,
		"count":        len(items),
		"items":        items,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	return string(out), nil
}

func parseTelegramSessionID(sessionID string) (int64, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if !strings.HasPrefix(sessionID, "telegram:") {
		return 0, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(sessionID, "telegram:"))
	if raw == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}
