package chathistory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRenderHistoryContext(t *testing.T) {
	t.Parallel()

	raw, err := RenderHistoryContext(ChannelTelegram, []ChatHistoryItem{{
		Kind:      KindInboundUser,
		MessageID: "101",
		SentAt:    time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC),
		Text:      "earlier message",
	}})
	if err != nil {
		t.Fatalf("RenderHistoryContext() error = %v", err)
	}

	var payload struct {
		ChatHistoryMessages []PromptMessageItem `json:"chat_history_messages"`
		Note                string              `json:"note"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(payload.ChatHistoryMessages) != 1 {
		t.Fatalf("len(chat_history_messages) = %d, want 1", len(payload.ChatHistoryMessages))
	}
	if payload.ChatHistoryMessages[0].Text != "earlier message" {
		t.Fatalf("text = %q, want %q", payload.ChatHistoryMessages[0].Text, "earlier message")
	}
	if !strings.Contains(payload.Note, "Historical messages only") {
		t.Fatalf("note = %q, want historical-context guidance", payload.Note)
	}
	var rawPayload map[string]any
	if err := json.Unmarshal([]byte(raw), &rawPayload); err != nil {
		t.Fatalf("Unmarshal(raw map) error = %v", err)
	}
	itemsRaw, ok := rawPayload["chat_history_messages"].([]any)
	if !ok || len(itemsRaw) != 1 {
		t.Fatalf("raw chat_history_messages shape = %#v", rawPayload["chat_history_messages"])
	}
	itemRaw, ok := itemsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("raw item shape = %#v", itemsRaw[0])
	}
	for _, field := range []string{"channel", "kind", "chat_id", "chat_type", "message_id", "reply_to_message_id"} {
		if _, exists := itemRaw[field]; exists {
			t.Fatalf("field %q should be omitted from prompt item", field)
		}
	}
}

func TestRenderHistoryContextEmptyReturnsBlank(t *testing.T) {
	t.Parallel()

	raw, err := RenderHistoryContext(ChannelTelegram, nil)
	if err != nil {
		t.Fatalf("RenderHistoryContext() error = %v", err)
	}
	if raw != "" {
		t.Fatalf("raw = %q, want blank", raw)
	}
}

func TestRenderCurrentMessage(t *testing.T) {
	t.Parallel()

	raw, err := RenderCurrentMessage(ChatHistoryItem{
		Channel:   ChannelSlack,
		Kind:      KindInboundUser,
		MessageID: "102",
		SentAt:    time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
		Text:      "Hi",
	})
	if err != nil {
		t.Fatalf("RenderCurrentMessage() error = %v", err)
	}

	var payload struct {
		CurrentMessage PromptMessageItem `json:"current_message"`
		Instruction    string            `json:"instruction"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.CurrentMessage.Text != "Hi" {
		t.Fatalf("text = %q, want %q", payload.CurrentMessage.Text, "Hi")
	}
	if !strings.Contains(payload.Instruction, "latest inbound user message") {
		t.Fatalf("instruction = %q, want latest-message guidance", payload.Instruction)
	}
	var rawPayload map[string]any
	if err := json.Unmarshal([]byte(raw), &rawPayload); err != nil {
		t.Fatalf("Unmarshal(raw map) error = %v", err)
	}
	itemRaw, ok := rawPayload["current_message"].(map[string]any)
	if !ok {
		t.Fatalf("raw current_message shape = %#v", rawPayload["current_message"])
	}
	for _, field := range []string{"channel", "kind", "chat_id", "chat_type", "message_id", "reply_to_message_id"} {
		if _, exists := itemRaw[field]; exists {
			t.Fatalf("field %q should be omitted from current_message", field)
		}
	}
}

func TestRenderCurrentMessageIncludesImages(t *testing.T) {
	t.Parallel()

	raw, err := RenderCurrentMessage(ChatHistoryItem{
		Channel:   ChannelSlack,
		Kind:      KindInboundUser,
		MessageID: "102",
		SentAt:    time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
		Text:      "look",
		Images: []ChatHistoryImage{{
			ID:                 "img_abc123",
			Path:               "workspace_dir/.mistermorph/images/slack/a.png",
			MIMEType:           "image/png",
			Width:              2,
			Height:             3,
			Bytes:              79,
			ContentSHA256:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			SourceMessageID:    "1739667600.000100",
			SourceAttachmentID: "F111",
			Description:        "a small test image",
			DescriptionSource:  "agent_final",
		}},
	})
	if err != nil {
		t.Fatalf("RenderCurrentMessage() error = %v", err)
	}

	var payload struct {
		CurrentMessage PromptMessageItem `json:"current_message"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(payload.CurrentMessage.Images) != 1 {
		t.Fatalf("images len = %d, want 1", len(payload.CurrentMessage.Images))
	}
	img := payload.CurrentMessage.Images[0]
	if img.ID != "img_abc123" || img.Path != "workspace_dir/.mistermorph/images/slack/a.png" {
		t.Fatalf("image identity mismatch: %#v", img)
	}
	if img.Width != 2 || img.Height != 3 || img.Bytes != 79 {
		t.Fatalf("image metadata mismatch: %#v", img)
	}
	if img.ContentSHA256 != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("image content hash mismatch: %#v", img)
	}
	if img.Description != "a small test image" || img.DescriptionSource != "agent_final" {
		t.Fatalf("image description mismatch: %#v", img)
	}
}
