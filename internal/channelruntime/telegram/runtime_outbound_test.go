package telegram

import (
	"testing"
	"time"

	"github.com/google/uuid"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

func TestTelegramOutboundEventFromBusMessage(t *testing.T) {
	sessionID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	payload, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "msg_1",
		Text:      "hello",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		SessionID: sessionID.String(),
		ReplyTo:   "123",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	event, err := telegramOutboundEventFromBusMessage(busruntime.BusMessage{
		ConversationKey: "tg:42_901",
		Topic:           busruntime.TopicChatMessage,
		PayloadBase64:   payload,
		CorrelationID:   "telegram:plan:42:1",
		Extensions: busruntime.MessageExtensions{
			ReplyTo:         "123",
			MessageThreadID: 901,
		},
	})
	if err != nil {
		t.Fatalf("telegramOutboundEventFromBusMessage() error = %v", err)
	}
	if event.ChatID != 42 {
		t.Fatalf("chat id = %d, want 42", event.ChatID)
	}
	if event.MessageThreadID != 901 {
		t.Fatalf("message thread id = %d, want 901", event.MessageThreadID)
	}
	if event.ReplyToMessageID != 123 {
		t.Fatalf("reply to message id = %d, want 123", event.ReplyToMessageID)
	}
	if event.Text != "hello" {
		t.Fatalf("text = %q, want hello", event.Text)
	}
	if event.Kind != "plan_progress" {
		t.Fatalf("kind = %q, want plan_progress", event.Kind)
	}
}

func TestTelegramOutboundKind(t *testing.T) {
	if got := telegramOutboundKind("telegram:error:1:2"); got != "error" {
		t.Fatalf("kind(error) = %q, want error", got)
	}
	if got := telegramOutboundKind("telegram:file_download_error:1:2"); got != "error" {
		t.Fatalf("kind(file_download_error) = %q, want error", got)
	}
	if got := telegramOutboundKind("telegram:message:1:2"); got != "message" {
		t.Fatalf("kind(message) = %q, want message", got)
	}
}

func TestNextTelegramPlanProgressStateAppendsLines(t *testing.T) {
	state, rendered := nextTelegramPlanProgressState(telegramPlanProgressEditState{}, "telegram:plan:1:1", "run web_search query")
	if state.CorrelationID != "telegram:plan:1:1" {
		t.Fatalf("correlation_id = %q", state.CorrelationID)
	}
	if len(state.Lines) != 1 || state.Lines[0].Text != "run web_search query" || state.Lines[0].Emoji != "🔎" {
		t.Fatalf("lines = %#v, want single step 1", state.Lines)
	}
	if rendered != "<blockquote expandable>🔎 1. run web_search query</blockquote>" {
		t.Fatalf("rendered = %q", rendered)
	}

	state.MessageID = 123
	next, rendered := nextTelegramPlanProgressState(state, "telegram:plan:1:1", "save via write_file")
	if next.MessageID != 123 {
		t.Fatalf("message_id = %d, want 123", next.MessageID)
	}
	if len(next.Lines) != 2 ||
		next.Lines[0].Text != "run web_search query" || next.Lines[0].Emoji != "🔎" ||
		next.Lines[1].Text != "save via write_file" || next.Lines[1].Emoji != "✍️" {
		t.Fatalf("lines = %#v, want step1+step2", next.Lines)
	}
	if rendered != "<blockquote expandable>✍️ 2. save via write_file<br>🔎 1. run web_search query</blockquote>" {
		t.Fatalf("rendered = %q, want numbered reverse lines", rendered)
	}
}

func TestNextTelegramPlanProgressStateResetsOnCorrelationChange(t *testing.T) {
	prev := telegramPlanProgressEditState{
		CorrelationID: "telegram:plan:1:1",
		MessageID:     99,
		Lines:         []telegramPlanProgressLine{{Text: "run web_search query", Emoji: "🔎"}},
	}
	next, rendered := nextTelegramPlanProgressState(prev, "telegram:plan:1:2", "fetch with url_fetch")
	if next.MessageID != 0 {
		t.Fatalf("message_id = %d, want 0", next.MessageID)
	}
	if len(next.Lines) != 1 || next.Lines[0].Text != "fetch with url_fetch" || next.Lines[0].Emoji != "🧭" {
		t.Fatalf("lines = %#v, want only step 2", next.Lines)
	}
	if rendered != "<blockquote expandable>🧭 1. fetch with url_fetch</blockquote>" {
		t.Fatalf("rendered = %q", rendered)
	}
}

func TestRenderTelegramPlanProgressExpandableEscapesHTML(t *testing.T) {
	got := renderTelegramPlanProgressExpandable([]telegramPlanProgressLine{
		{Text: "use read_file <b>x</b>", Emoji: "📖"},
	})
	if got != "<blockquote expandable>📖 1. use read_file &lt;b&gt;x&lt;/b&gt;</blockquote>" {
		t.Fatalf("rendered = %q", got)
	}
}

func TestEmojiForTelegramPlanStep(t *testing.T) {
	tests := []struct {
		step string
		want string
	}{
		{step: "do web_search now", want: "🔎"},
		{step: "please url_fetch page", want: "🧭"},
		{step: "read_file config", want: "📖"},
		{step: "write_file result", want: "✍️"},
		{step: "telegram_send_file something", want: "🗂️"},
		{step: "telegram_send_photo something", want: "📷"},
		{step: "telegram_send_voice something", want: "🎙️"},
		{step: "run bash script", want: "🧑‍💻"},
		{step: "todo_update next steps", want: "🗓️"},
		{step: "use contacts_send", want: "✉️"},
	}
	for _, tc := range tests {
		if got := emojiForTelegramPlanStep(tc.step); got != tc.want {
			t.Fatalf("emojiForTelegramPlanStep(%q) = %q, want %q", tc.step, got, tc.want)
		}
	}
}

func TestEmojiForTelegramPlanStepFallback(t *testing.T) {
	got := emojiForTelegramPlanStep("plain natural language step")
	if got != "💭" && got != "🤔" {
		t.Fatalf("fallback emoji = %q, want one of 💭/🤔", got)
	}
}
