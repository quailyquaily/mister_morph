package maepcmd

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/maep"
)

func TestSummarizePayload_JSONNotTruncated(t *testing.T) {
	longText := strings.Repeat("x", 220)
	payload := map[string]any{
		"message_id": "msg_1",
		"text":       longText,
		"sent_at":    "2026-02-07T04:31:30Z",
	}
	raw, _ := json.Marshal(payload)

	got := summarizePayload("application/json", base64.RawURLEncoding.EncodeToString(raw))
	if strings.Contains(got, "...") {
		t.Fatalf("expected full payload without truncation, got %q", got)
	}
	if !strings.Contains(got, longText) {
		t.Fatalf("expected summarized payload to contain full text")
	}
}

func TestSummarizePayload_TextNotTruncated(t *testing.T) {
	longText := strings.Repeat("a", 220)
	got := summarizePayload("text/plain", base64.RawURLEncoding.EncodeToString([]byte(longText)))
	if got != longText {
		t.Fatalf("text payload mismatch: got len=%d want len=%d", len(got), len(longText))
	}
}

func TestSummarizePayload_EnvelopePrettyJSON(t *testing.T) {
	payload := map[string]any{
		"message_id": "msg_1",
		"text":       "hello",
		"sent_at":    "2026-02-07T04:31:30Z",
	}
	raw, _ := json.Marshal(payload)
	got := summarizePayload("application/json", base64.RawURLEncoding.EncodeToString(raw))
	if !strings.Contains(got, "\n  \"message_id\"") {
		t.Fatalf("expected pretty envelope json, got %q", got)
	}
}

func TestSummarizePayload_NonEnvelopeJSONCompact(t *testing.T) {
	payload := map[string]any{
		"foo": "bar",
	}
	raw, _ := json.Marshal(payload)
	got := summarizePayload("application/json", base64.RawURLEncoding.EncodeToString(raw))
	if strings.Contains(got, "\n") {
		t.Fatalf("expected compact json for non-envelope payload, got %q", got)
	}
}

func TestWriteInboxRecords_BlockLayout(t *testing.T) {
	payload := map[string]any{
		"message_id": "msg_1",
		"text":       "hello",
		"sent_at":    "2026-02-07T04:31:30Z",
	}
	raw, _ := json.Marshal(payload)
	record := maep.InboxMessage{
		FromPeerID:     "12D3KooX",
		Topic:          "dm.reply.v1",
		ContentType:    "application/json",
		PayloadBase64:  base64.RawURLEncoding.EncodeToString(raw),
		IdempotencyKey: "reply:1",
		SessionID:      "12D3KooX::dialogue.v1",
	}
	var b strings.Builder
	writeInboxRecords(&b, []maep.InboxMessage{record})
	out := b.String()
	for _, want := range []string{
		"[1]\n",
		"from_peer_id: 12D3KooX\n",
		"topic: dm.reply.v1\n",
		"session_id: 12D3KooX::dialogue.v1\n",
		"content_type: application/json\n",
		"payload:\n",
		"  {\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("writeInboxRecords() missing %q in output:\n%s", want, out)
		}
	}
}

func TestIndentBlock(t *testing.T) {
	got := indentBlock("a\nb", "  ")
	if got != "  a\n  b" {
		t.Fatalf("indentBlock() mismatch: got %q", got)
	}
}

func TestWriteOutboxRecords_BlockLayout(t *testing.T) {
	payload := map[string]any{
		"message_id": "msg_1",
		"text":       "hello",
		"sent_at":    "2026-02-07T04:31:30Z",
	}
	raw, _ := json.Marshal(payload)
	record := maep.OutboxMessage{
		ToPeerID:       "12D3KooY",
		Topic:          "dm.reply.v1",
		ContentType:    "application/json",
		PayloadBase64:  base64.RawURLEncoding.EncodeToString(raw),
		IdempotencyKey: "reply:2",
		SessionID:      "12D3KooY::dialogue.v1",
	}
	var b strings.Builder
	writeOutboxRecords(&b, []maep.OutboxMessage{record})
	out := b.String()
	for _, want := range []string{
		"[1]\n",
		"to_peer_id: 12D3KooY\n",
		"topic: dm.reply.v1\n",
		"session_id: 12D3KooY::dialogue.v1\n",
		"content_type: application/json\n",
		"payload:\n",
		"  {\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("writeOutboxRecords() missing %q in output:\n%s", want, out)
		}
	}
}
