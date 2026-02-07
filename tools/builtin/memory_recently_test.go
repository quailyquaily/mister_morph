package builtin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/memory"
)

func TestMemoryRecentlyTool_ReadsRecentItems(t *testing.T) {
	memDir := t.TempDir()
	mgr := memory.NewManager(memDir, 7)
	_, err := mgr.WriteShortTerm(time.Now().UTC(), memory.ShortTermContent{
		SessionSummary: []memory.KVItem{{Title: "summary", Value: "chat update"}},
		Tasks:          []memory.TaskItem{{Text: "follow up", Done: false}},
	}, "chat update", memory.WriteMeta{
		SessionID:       "telegram:-100123456",
		Source:          "telegram",
		Channel:         "group",
		ContactID:       "tg:@alice",
		ContactNickname: "Alice",
	})
	if err != nil {
		t.Fatalf("WriteShortTerm() error = %v", err)
	}

	tool := NewMemoryRecentlyTool(true, memDir, 3, 10)
	out, err := tool.Execute(context.Background(), map[string]any{
		"days":  3,
		"limit": 5,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var parsed struct {
		Count int `json:"count"`
		Items []struct {
			ContactID        string `json:"contact_id"`
			TelegramChatID   int64  `json:"telegram_chat_id"`
			TelegramChatType string `json:"telegram_chat_type"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if parsed.Count <= 0 || len(parsed.Items) == 0 {
		t.Fatalf("expected at least one item, got count=%d", parsed.Count)
	}
	if parsed.Items[0].ContactID != "tg:@alice" {
		t.Fatalf("contact_id mismatch: got %q", parsed.Items[0].ContactID)
	}
	if parsed.Items[0].TelegramChatID != -100123456 {
		t.Fatalf("telegram_chat_id mismatch: got %d", parsed.Items[0].TelegramChatID)
	}
	if parsed.Items[0].TelegramChatType != "group" {
		t.Fatalf("telegram_chat_type mismatch: got %q", parsed.Items[0].TelegramChatType)
	}
}
