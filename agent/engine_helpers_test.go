package agent

import "testing"

func TestToolArgsSummary_ContactsSendSafeSummary(t *testing.T) {
	opts := DefaultLogOptions()
	params := map[string]any{
		"contact_id":     "telegram:alice",
		"topic":          "share.proactive.v1",
		"message_text":   "private content should not be logged",
		"source_chat_id": float64(12345),
	}

	got := toolArgsSummary("contacts_send", params, opts)
	if got == nil {
		t.Fatalf("summary should not be nil")
	}
	if got["contact_id"] != "telegram:alice" {
		t.Fatalf("unexpected contact_id summary: %#v", got["contact_id"])
	}
	if got["topic"] != "share.proactive.v1" {
		t.Fatalf("unexpected topic summary: %#v", got["topic"])
	}
	if v, ok := got["has_message_text"].(bool); !ok || !v {
		t.Fatalf("expected has_message_text=true, got %#v", got["has_message_text"])
	}
	if _, exists := got["message_text"]; exists {
		t.Fatalf("must not log raw message_text")
	}
}

func TestToolArgsSummary_ContactsFeedbackAndList(t *testing.T) {
	opts := DefaultLogOptions()

	feedback := toolArgsSummary("contacts_feedback_update", map[string]any{
		"contact_id":  "telegram:alice",
		"signal":      "positive",
		"end_session": true,
	}, opts)
	if feedback == nil {
		t.Fatalf("feedback summary should not be nil")
	}
	if feedback["signal"] != "positive" {
		t.Fatalf("unexpected signal summary: %#v", feedback["signal"])
	}

	list := toolArgsSummary("contacts_list", map[string]any{
		"status": "active",
		"limit":  float64(10),
	}, opts)
	if list == nil {
		t.Fatalf("list summary should not be nil")
	}
	if list["status"] != "active" {
		t.Fatalf("unexpected status summary: %#v", list["status"])
	}
	if list["limit"] != int64(10) {
		t.Fatalf("unexpected limit summary: %#v", list["limit"])
	}
}
