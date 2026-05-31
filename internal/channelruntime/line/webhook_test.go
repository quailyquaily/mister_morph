package line

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

func TestVerifyLineWebhookSignature(t *testing.T) {
	t.Parallel()

	secret := "line_secret"
	body := []byte(`{"events":[]}`)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	valid := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !verifyLineWebhookSignature(secret, body, valid) {
		t.Fatalf("verifyLineWebhookSignature() = false, want true")
	}
	if verifyLineWebhookSignature(secret, body, "invalid_signature") {
		t.Fatalf("verifyLineWebhookSignature() with invalid signature = true, want false")
	}
}

func TestInboundMessageFromWebhookEvent_Group(t *testing.T) {
	t.Parallel()

	event := lineWebhookEvent{
		Type:           "message",
		Timestamp:      1760000000123,
		ReplyToken:     "reply_tok",
		WebhookEventID: "ev_001",
		Source: lineWebhookSource{
			Type:    "group",
			GroupID: "Cgroup123",
			UserID:  "U123",
		},
		Message: lineWebhookMessage{
			ID:   "m_1001",
			Type: "text",
			Text: "hello line",
			Mention: &lineWebhookMention{
				Mentionees: []lineWebhookMentionee{
					{Type: "user", UserID: "U123"},
					{Type: "user", UserID: "U456"},
					{Type: "user", UserID: "U123"},
				},
			},
		},
	}
	msg, ok, err := inboundMessageFromWebhookEvent(event, map[string]bool{})
	if err != nil {
		t.Fatalf("inboundMessageFromWebhookEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("inboundMessageFromWebhookEvent() ok=false, want true")
	}
	if msg.ChatID != "Cgroup123" {
		t.Fatalf("chat_id = %q, want %q", msg.ChatID, "Cgroup123")
	}
	if msg.ChatType != "group" {
		t.Fatalf("chat_type = %q, want %q", msg.ChatType, "group")
	}
	if msg.FromUserID != "U123" {
		t.Fatalf("from_user_id = %q, want %q", msg.FromUserID, "U123")
	}
	if msg.MessageID != "m_1001" {
		t.Fatalf("message_id = %q, want %q", msg.MessageID, "m_1001")
	}
	if msg.ReplyToken != "reply_tok" {
		t.Fatalf("reply_token = %q, want %q", msg.ReplyToken, "reply_tok")
	}
	if msg.EventID != "ev_001" {
		t.Fatalf("event_id = %q, want %q", msg.EventID, "ev_001")
	}
	if len(msg.MentionUsers) != 2 {
		t.Fatalf("mention_users len = %d, want 2", len(msg.MentionUsers))
	}
	wantSentAt := time.UnixMilli(1760000000123).UTC()
	if !msg.SentAt.Equal(wantSentAt) {
		t.Fatalf("sent_at = %s, want %s", msg.SentAt.Format(time.RFC3339Nano), wantSentAt.Format(time.RFC3339Nano))
	}
}

func TestInboundMessageFromWebhookEvent_Private(t *testing.T) {
	t.Parallel()

	event := lineWebhookEvent{
		Type: "message",
		Source: lineWebhookSource{
			Type:   "user",
			UserID: "Uprivate001",
		},
		Message: lineWebhookMessage{
			ID:   "m_2001",
			Type: "text",
			Text: "hi",
		},
	}
	msg, ok, err := inboundMessageFromWebhookEvent(event, map[string]bool{})
	if err != nil {
		t.Fatalf("inboundMessageFromWebhookEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("inboundMessageFromWebhookEvent() ok=false, want true")
	}
	if msg.ChatType != "private" {
		t.Fatalf("chat_type = %q, want %q", msg.ChatType, "private")
	}
	if msg.ChatID != "Uprivate001" {
		t.Fatalf("chat_id = %q, want %q", msg.ChatID, "Uprivate001")
	}
}

func TestInboundMessageFromWebhookEvent_RoomIgnored(t *testing.T) {
	t.Parallel()

	event := lineWebhookEvent{
		Type: "message",
		Source: lineWebhookSource{
			Type:   "room",
			RoomID: "R1",
			UserID: "U1",
		},
		Message: lineWebhookMessage{
			ID:   "m_3001",
			Type: "text",
			Text: "hello",
		},
	}
	_, ok, err := inboundMessageFromWebhookEvent(event, map[string]bool{})
	if err != nil {
		t.Fatalf("inboundMessageFromWebhookEvent() error = %v", err)
	}
	if ok {
		t.Fatalf("inboundMessageFromWebhookEvent() ok=true, want false")
	}
}

func TestInboundMessageFromWebhookEvent_GroupAllowlist(t *testing.T) {
	t.Parallel()

	event := lineWebhookEvent{
		Type: "message",
		Source: lineWebhookSource{
			Type:    "group",
			GroupID: "Cgroup_denied",
			UserID:  "U123",
		},
		Message: lineWebhookMessage{
			ID:   "m_4001",
			Type: "text",
			Text: "hello",
		},
	}
	_, ok, err := inboundMessageFromWebhookEvent(event, map[string]bool{"Cgroup_allowed": true})
	if err != nil {
		t.Fatalf("inboundMessageFromWebhookEvent() error = %v", err)
	}
	if ok {
		t.Fatalf("inboundMessageFromWebhookEvent() ok=true, want false")
	}
}

func TestInboundMessageFromWebhookEvent_ImageEnabled(t *testing.T) {
	t.Parallel()

	event := lineWebhookEvent{
		Type: "message",
		Source: lineWebhookSource{
			Type:   "user",
			UserID: "Uprivate001",
		},
		Message: lineWebhookMessage{
			ID:   "m_img_1",
			Type: "image",
		},
	}
	msg, ok, err := inboundMessageFromWebhookEvent(event, map[string]bool{})
	if err != nil {
		t.Fatalf("inboundMessageFromWebhookEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("inboundMessageFromWebhookEvent() ok=false, want true")
	}
	if msg.Text != "Please process the uploaded image." {
		t.Fatalf("text = %q, want %q", msg.Text, "Please process the uploaded image.")
	}
	if len(msg.ImagePaths) != 0 {
		t.Fatalf("image_paths len = %d, want 0", len(msg.ImagePaths))
	}
	if !msg.ImagePending {
		t.Fatalf("image_pending = false, want true")
	}
}
