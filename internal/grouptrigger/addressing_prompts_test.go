package grouptrigger

import (
	"encoding/json"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
)

func TestRenderAddressingPrompts_TrimHistoryToLatestThree(t *testing.T) {
	history := []chathistory.ChatHistoryItem{
		{MessageID: "1", Text: "m1"},
		{MessageID: "2", Text: "m2"},
		{MessageID: "3", Text: "m3"},
		{MessageID: "4", Text: "m4"},
		{MessageID: "5", Text: "m5"},
	}

	_, userPrompt, err := RenderAddressingPrompts("persona", "", map[string]any{"text": "current"}, history)
	if err != nil {
		t.Fatalf("RenderAddressingPrompts() error = %v", err)
	}

	var payload struct {
		ChatHistoryMessages []chathistory.PromptMessageItem `json:"chat_history_messages"`
	}
	if err := json.Unmarshal([]byte(userPrompt), &payload); err != nil {
		t.Fatalf("json.Unmarshal(userPrompt) error = %v", err)
	}
	if len(payload.ChatHistoryMessages) != 3 {
		t.Fatalf("chat_history_messages len = %d, want 3", len(payload.ChatHistoryMessages))
	}

	wantTexts := []string{"m3", "m4", "m5"}
	for i, want := range wantTexts {
		if got := payload.ChatHistoryMessages[i].Text; got != want {
			t.Fatalf("chat_history_messages[%d].text = %q, want %q", i, got, want)
		}
	}

	var rawPayload map[string]any
	if err := json.Unmarshal([]byte(userPrompt), &rawPayload); err != nil {
		t.Fatalf("json.Unmarshal(raw userPrompt) error = %v", err)
	}
	itemsRaw, ok := rawPayload["chat_history_messages"].([]any)
	if !ok || len(itemsRaw) != 3 {
		t.Fatalf("raw chat_history_messages shape = %#v", rawPayload["chat_history_messages"])
	}
	itemRaw, ok := itemsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("raw item shape = %#v", itemsRaw[0])
	}
	if _, exists := itemRaw["message_id"]; exists {
		t.Fatalf("message_id should be omitted from addressing history prompt")
	}
}
