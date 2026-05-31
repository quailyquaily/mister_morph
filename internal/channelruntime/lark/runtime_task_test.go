package lark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/tools"
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
	}, "gpt-5.2", nil)
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
	}, "gpt-5.2", nil)
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

func TestBuildLarkPromptMessagesWithImageParts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	if err := os.WriteFile(path, []byte("png-data"), 0o600); err != nil {
		t.Fatalf("write image: %v", err)
	}

	historyMsg, currentMsg, err := buildLarkPromptMessages(nil, larkJob{
		ChatID:      "oc_123",
		ChatType:    "group",
		MessageID:   "102",
		FromUserID:  "ou_123",
		DisplayName: "Alice",
		Text:        "latest",
		ImagePaths:  []string{path},
		SentAt:      time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
	}, "gpt-5.2", nil)
	if err != nil {
		t.Fatalf("buildLarkPromptMessages() error = %v", err)
	}
	if historyMsg != nil {
		t.Fatalf("historyMsg should be nil")
	}
	if currentMsg == nil {
		t.Fatalf("currentMsg = nil")
	}
	if len(currentMsg.Parts) != 2 {
		t.Fatalf("current parts len = %d, want 2", len(currentMsg.Parts))
	}
	if currentMsg.Parts[1].MIMEType != "image/png" {
		t.Fatalf("image MIME = %q, want image/png", currentMsg.Parts[1].MIMEType)
	}
}

func TestNewLarkInboundHistoryItemIncludesImages(t *testing.T) {
	t.Parallel()

	item := newLarkInboundHistoryItem(larkJob{
		ChatID:     "oc_123",
		ChatType:   "group",
		MessageID:  "om_1",
		FromUserID: "ou_123",
		Text:       "latest",
		Images: []chathistory.ChatHistoryImage{{
			ID:                 "img_lark_1",
			Path:               "workspace_dir/.mistermorph/images/lark/a.png",
			SourceMessageID:    "om_1",
			SourceAttachmentID: "img_v2_1",
		}},
	})
	if len(item.Images) != 1 {
		t.Fatalf("images len = %d, want 1", len(item.Images))
	}
	if item.Images[0].ID != "img_lark_1" || item.Images[0].SourceAttachmentID != "img_v2_1" {
		t.Fatalf("image mismatch: %#v", item.Images[0])
	}
}

func TestRegisterLarkChannelTools(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reactTool := registerLarkChannelTools(reg, &stubRuntimeLarkToolAPI{}, "oc_123", "om_123", t.TempDir(), 1024)
	if reactTool == nil {
		t.Fatalf("reactTool = nil")
	}
	for _, name := range []string{"lark_send_file", "lark_send_photo", "lark_send_voice", "message_react"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("registry missing %s; names=%s", name, reg.ToolNames())
		}
	}
}

type stubRuntimeLarkToolAPI struct{}

func (stubRuntimeLarkToolAPI) SendFile(context.Context, string, string, string, string) error {
	return nil
}

func (stubRuntimeLarkToolAPI) SendPhoto(context.Context, string, string, string, string) error {
	return nil
}

func (stubRuntimeLarkToolAPI) SendVoice(context.Context, string, string, string) error {
	return nil
}

func (stubRuntimeLarkToolAPI) SetEmojiReaction(context.Context, string, string) error {
	return nil
}
