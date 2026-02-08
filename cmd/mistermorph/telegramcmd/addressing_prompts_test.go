package telegramcmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderTelegramAddressingPrompts(t *testing.T) {
	sys, user, err := renderTelegramAddressingPrompts("my_bot", []string{"mybot", "morph"}, "hey mybot do this")
	if err != nil {
		t.Fatalf("renderTelegramAddressingPrompts() error = %v", err)
	}
	if !strings.Contains(sys, "strict classifier for a Telegram chatbot") {
		t.Fatalf("unexpected system prompt: %q", sys)
	}

	var payload struct {
		BotUsername string   `json:"bot_username"`
		Aliases     []string `json:"aliases"`
		Message     string   `json:"message"`
		Note        string   `json:"note"`
	}
	if err := json.Unmarshal([]byte(user), &payload); err != nil {
		t.Fatalf("user prompt is not valid json: %v", err)
	}
	if payload.BotUsername != "my_bot" {
		t.Fatalf("bot_username = %q, want my_bot", payload.BotUsername)
	}
	if len(payload.Aliases) != 2 {
		t.Fatalf("aliases len = %d, want 2", len(payload.Aliases))
	}
	if strings.TrimSpace(payload.Message) == "" {
		t.Fatalf("message should not be empty")
	}
	if strings.TrimSpace(payload.Note) == "" {
		t.Fatalf("note should not be empty")
	}
}
