package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/quailyquaily/mistermorph/internal/channels"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

func TestUntriggeredRecorderWritesMinimalEvent(t *testing.T) {
	dir := t.TempDir()
	recorder, err := NewUntriggeredRecorder(dir, 0)
	if err != nil {
		t.Fatalf("NewUntriggeredRecorder() error = %v", err)
	}
	recorder.now = func() time.Time {
		return time.Date(2026, 8, 7, 3, 4, 5, 6000000, time.UTC)
	}

	err = recorder.Record(UntriggeredMessage{
		Channel:         channels.Telegram,
		ConversationKey: " tg:-1001_8 ",
		MessageID:       " 42 ",
		SenderID:        " 10001 ",
		SentAt:          time.Date(2026, 8, 7, 3, 4, 4, 0, time.FixedZone("JST", 9*60*60)),
		Text:            "  first\r\nsecond\r  ",
		HasAttachment:   true,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var events []domainjournal.Event
	if err := domainjournal.ReplayDir(dir, func(rec domainjournal.Record) error {
		events = append(events, rec.Event)
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	event := events[0]
	if event.Domain != "conversation" || event.Type != "untriggered_inbound" || event.SchemaVersion != 1 {
		t.Fatalf("event envelope = %#v", event)
	}
	if event.Time != "2026-08-07T03:04:05.006Z" {
		t.Fatalf("event time = %q", event.Time)
	}
	if event.Trace.Runtime != channels.Telegram {
		t.Fatalf("trace runtime = %q, want telegram", event.Trace.Runtime)
	}

	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v", err)
	}
	want := map[string]any{
		"channel":          "telegram",
		"conversation_key": "tg:-1001_8",
		"message_id":       "42",
		"sender_id":        "10001",
		"sent_at":          "2026-08-06T18:04:04Z",
		"text":             "first\nsecond",
		"has_attachment":   true,
	}
	if len(payload) != len(want) {
		t.Fatalf("payload = %#v, want only %#v", payload, want)
	}
	for key, wantValue := range want {
		if got := payload[key]; got != wantValue {
			t.Fatalf("payload[%q] = %#v, want %#v", key, got, wantValue)
		}
	}
}

func TestUntriggeredRecorderTruncatesUTF8(t *testing.T) {
	dir := t.TempDir()
	recorder, err := NewUntriggeredRecorder(dir, 0)
	if err != nil {
		t.Fatalf("NewUntriggeredRecorder() error = %v", err)
	}
	t.Cleanup(func() { _ = recorder.Close() })

	if err := recorder.Record(UntriggeredMessage{
		Channel:         channels.Slack,
		ConversationKey: "slack:C1:thread:1.2",
		MessageID:       "1.3",
		SentAt:          time.Date(2026, 8, 7, 3, 4, 4, 0, time.UTC),
		Text:            strings.Repeat("界", 683),
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	var payload struct {
		Text          string `json:"text"`
		TextTruncated bool   `json:"text_truncated"`
	}
	if err := domainjournal.ReplayDir(dir, func(rec domainjournal.Record) error {
		return json.Unmarshal(rec.Event.Payload, &payload)
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	if len(payload.Text) > 2048 || !utf8.ValidString(payload.Text) {
		t.Fatalf("stored text bytes = %d, valid UTF-8 = %v", len(payload.Text), utf8.ValidString(payload.Text))
	}
	if !payload.TextTruncated {
		t.Fatal("text_truncated = false, want true")
	}
}

func TestUntriggeredRecorderSkipsEmptyMessage(t *testing.T) {
	dir := t.TempDir()
	recorder, err := NewUntriggeredRecorder(dir, 0)
	if err != nil {
		t.Fatalf("NewUntriggeredRecorder() error = %v", err)
	}
	t.Cleanup(func() { _ = recorder.Close() })

	if err := recorder.Record(UntriggeredMessage{
		Channel:         channels.Lark,
		ConversationKey: "oc_1",
		MessageID:       "om_1",
		SentAt:          time.Now(),
		Text:            " \r\n ",
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	count := 0
	if err := domainjournal.ReplayDir(dir, func(domainjournal.Record) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("event count = %d, want 0", count)
	}
}
