package lark

import (
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
)

func TestBuildLarkPromptMessagesSeparatesHistoryAndCurrent(t *testing.T) {
	t.Parallel()

	historyMsg, currentMsg, err := buildLarkPromptMessages([]chathistory.ChatHistoryItem{{
		Channel:   chathistory.ChannelLark,
		Kind:      chathistory.KindInboundUser,
		MessageID: "101",
		SentAt:    time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC),
		Text:      "earlier",
	}}, larkJob{
		ChatID:      "oc_123",
		ChatType:    "group",
		MessageID:   "102",
		FromUserID:  "ou_123",
		DisplayName: "Alice",
		Text:        "latest",
		SentAt:      time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("buildLarkPromptMessages() error = %v", err)
	}
	if historyMsg == nil {
		t.Fatalf("historyMsg = nil")
	}
	if strings.Contains(historyMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("history should not contain latest message: %s", historyMsg.Content)
	}
	if !strings.Contains(historyMsg.Content, "\"text\": \"earlier\"") {
		t.Fatalf("history should contain prior message: %s", historyMsg.Content)
	}
	if currentMsg == nil {
		t.Fatalf("currentMsg = nil")
	}
	if !strings.Contains(currentMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("current message should contain latest text: %s", currentMsg.Content)
	}
}

func TestContactsSendRuntimeContextForLarkPrivateChat(t *testing.T) {
	t.Parallel()

	ctx := contactsSendRuntimeContextForLark(larkJob{
		ChatID:     "oc_123",
		ChatType:   "p2p",
		FromUserID: "ou_123",
	})
	if len(ctx.ForbiddenTargetIDs) != 2 {
		t.Fatalf("forbidden_target_ids len = %d, want 2", len(ctx.ForbiddenTargetIDs))
	}
	if ctx.ForbiddenTargetIDs[0] != "lark_user:ou_123" {
		t.Fatalf("forbidden_target_ids[0] = %q, want %q", ctx.ForbiddenTargetIDs[0], "lark_user:ou_123")
	}
	if ctx.ForbiddenTargetIDs[1] != "lark:oc_123" {
		t.Fatalf("forbidden_target_ids[1] = %q, want %q", ctx.ForbiddenTargetIDs[1], "lark:oc_123")
	}
}

func TestBuildLarkPromptMessagesOmitsEmptyHistory(t *testing.T) {
	t.Parallel()

	historyMsg, currentMsg, err := buildLarkPromptMessages(nil, larkJob{
		ChatID:      "oc_123",
		ChatType:    "group",
		MessageID:   "102",
		FromUserID:  "ou_123",
		DisplayName: "Alice",
		Text:        "latest",
		SentAt:      time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("buildLarkPromptMessages() error = %v", err)
	}
	if historyMsg != nil {
		t.Fatalf("historyMsg should be nil when history is empty")
	}
	if currentMsg == nil || !strings.Contains(currentMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("current message should still be present: %#v", currentMsg)
	}
}
