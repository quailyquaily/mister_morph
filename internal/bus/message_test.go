package bus

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIsDialogueTopic(t *testing.T) {
	cases := []struct {
		topic string
		want  bool
	}{
		{topic: TopicShareProactiveV1, want: true},
		{topic: TopicDMCheckinV1, want: true},
		{topic: TopicDMReplyV1, want: true},
		{topic: TopicChatMessage, want: true},
		{topic: "agent.status.v1", want: false},
	}
	for _, tc := range cases {
		if got := IsDialogueTopic(tc.topic); got != tc.want {
			t.Fatalf("IsDialogueTopic(%q) = %v, want %v", tc.topic, got, tc.want)
		}
	}
}

func TestMessageEnvelopeValidate_DialogueRequiresSessionID(t *testing.T) {
	env := MessageEnvelope{
		MessageID: "msg_01",
		Text:      "hello",
		SentAt:    "2026-02-08T10:00:00Z",
	}
	err := env.Validate(TopicChatMessage)
	if err == nil || !strings.Contains(err.Error(), "session_id is required") {
		t.Fatalf("Validate() error = %v, want session_id required", err)
	}
}

func TestMessageEnvelopeValidate_RejectsNonUUIDv7(t *testing.T) {
	env := MessageEnvelope{
		MessageID: "msg_01",
		Text:      "hello",
		SentAt:    "2026-02-08T10:00:00Z",
		SessionID: uuid.NewString(), // v4 by default
	}
	err := env.Validate(TopicChatMessage)
	if err == nil || !strings.Contains(err.Error(), "uuid_v7") {
		t.Fatalf("Validate() error = %v, want uuid_v7", err)
	}
}

func TestEncodeDecodeMessageEnvelope_RoundTrip(t *testing.T) {
	sessionID := mustUUIDv7(t)
	env := MessageEnvelope{
		MessageID: "msg_01",
		Text:      "hello",
		SentAt:    "2026-02-08T10:00:00Z",
		SessionID: sessionID,
		ReplyTo:   "msg_00",
	}

	raw, err := EncodeMessageEnvelope(TopicChatMessage, env)
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}

	got, err := DecodeMessageEnvelope(TopicChatMessage, raw)
	if err != nil {
		t.Fatalf("DecodeMessageEnvelope() error = %v", err)
	}
	if got != env {
		t.Fatalf("decoded envelope mismatch: got=%+v want=%+v", got, env)
	}
}

func TestDecodeMessageEnvelope_RejectsUnknownField(t *testing.T) {
	obj := map[string]any{
		"message_id": "msg_01",
		"text":       "hello",
		"sent_at":    "2026-02-08T10:00:00Z",
		"session_id": mustUUIDv7(t),
		"unknown":    "x",
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	b64 := base64.RawURLEncoding.EncodeToString(raw)
	_, err = DecodeMessageEnvelope(TopicChatMessage, b64)
	if err == nil || !strings.Contains(err.Error(), "invalid message envelope json") {
		t.Fatalf("DecodeMessageEnvelope() error = %v, want invalid message envelope json", err)
	}
}

func TestMessageValidate_Success(t *testing.T) {
	msg := validMessage(t)
	if err := msg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestMessageValidate_RejectsInvalidDirection(t *testing.T) {
	msg := validMessage(t)
	msg.Direction = Direction("sideway")
	err := msg.Validate()
	if err == nil || !strings.Contains(err.Error(), "direction must be inbound|outbound") {
		t.Fatalf("Validate() error = %v, want direction error", err)
	}
}

func TestMessageValidate_AllowsMinimalRequiredFields(t *testing.T) {
	conversationKey, err := BuildTelegramChatConversationKey("123")
	if err != nil {
		t.Fatalf("BuildTelegramChatConversationKey() error = %v", err)
	}
	payload, err := EncodeMessageEnvelope(TopicChatMessage, MessageEnvelope{
		MessageID: "msg_01",
		Text:      "hello",
		SentAt:    "2026-02-08T10:00:00Z",
		SessionID: mustUUIDv7(t),
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	msg := BusMessage{
		Topic:           TopicChatMessage,
		ConversationKey: conversationKey,
		IdempotencyKey:  "idem_01",
		PayloadBase64:   payload,
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestMessageValidate_RejectsUnknownTopic(t *testing.T) {
	msg := validMessage(t)
	msg.Topic = "agent.status.v1"
	err := msg.Validate()
	if err == nil || !strings.Contains(err.Error(), "INVALID_TOPIC") {
		t.Fatalf("Validate() error = %v, want invalid topic code", err)
	}
}

func TestMessageValidate_RejectsPayloadMismatch(t *testing.T) {
	env := MessageEnvelope{
		MessageID: "msg_01",
		Text:      "hello",
		SentAt:    "2026-02-08T10:00:00Z",
	}
	payload, err := EncodeMessageEnvelope("agent.status.v1", env)
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}

	msg := validMessage(t)
	msg.Topic = TopicChatMessage
	msg.PayloadBase64 = payload

	err = msg.Validate()
	if err == nil || !strings.Contains(err.Error(), "session_id is required") {
		t.Fatalf("Validate() error = %v, want session_id required", err)
	}
}

func TestMessageValidate_AllowsSlackExtensions(t *testing.T) {
	msg := validMessage(t)
	msg.Channel = ChannelSlack
	msg.ConversationKey = "slack:T111:C222"
	msg.Extensions.TeamID = "T111"
	msg.Extensions.ChannelID = "C222"
	msg.Extensions.FromUserRef = "U333"
	msg.Extensions.ThreadTS = "1739500000.123456"
	msg.Extensions.EventID = "Ev01"
	if err := msg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestMessageValidate_AllowsLineChannel(t *testing.T) {
	msg := validMessage(t)
	msg.Channel = ChannelLine
	msg.ConversationKey = "line:Cgroup123"
	if err := msg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestMessageValidate_RejectsInvalidSlackExtension(t *testing.T) {
	msg := validMessage(t)
	msg.Extensions.TeamID = " T111"
	err := msg.Validate()
	if err == nil || !strings.Contains(err.Error(), "extensions.team_id") {
		t.Fatalf("Validate() error = %v, want extensions.team_id error", err)
	}
}

func TestMessageValidate_AllowsImagePathsExtension(t *testing.T) {
	msg := validMessage(t)
	msg.Extensions.ImagePaths = []string{"/tmp/a.png", "/tmp/b.jpg"}
	if err := msg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestMessageValidate_RejectsInvalidImagePathExtension(t *testing.T) {
	msg := validMessage(t)
	msg.Extensions.ImagePaths = []string{" /tmp/a.png"}
	err := msg.Validate()
	if err == nil || !strings.Contains(err.Error(), "extensions.image_paths[0]") {
		t.Fatalf("Validate() error = %v, want extensions.image_paths[0] error", err)
	}
}

func TestMessageValidate_AllowsImageKeysExtension(t *testing.T) {
	msg := validMessage(t)
	msg.Extensions.ImageKeys = []string{"img_123", "img_456"}
	if err := msg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestMessageValidate_RejectsInvalidImageKeyExtension(t *testing.T) {
	msg := validMessage(t)
	msg.Extensions.ImageKeys = []string{" img_123"}
	err := msg.Validate()
	if err == nil || !strings.Contains(err.Error(), "extensions.image_keys[0]") {
		t.Fatalf("Validate() error = %v, want extensions.image_keys[0] error", err)
	}
}

func validMessage(t *testing.T) BusMessage {
	t.Helper()
	payload, err := EncodeMessageEnvelope(TopicChatMessage, MessageEnvelope{
		MessageID: "msg_01",
		Text:      "hello",
		SentAt:    "2026-02-08T10:00:00Z",
		SessionID: mustUUIDv7(t),
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	return BusMessage{
		ID:              "bus_01",
		Direction:       DirectionInbound,
		Channel:         ChannelTelegram,
		Topic:           TopicChatMessage,
		ConversationKey: "tg:123",
		ParticipantKey:  "tg:user:42",
		IdempotencyKey:  "idem_01",
		CorrelationID:   "corr_01",
		PayloadBase64:   payload,
		CreatedAt:       time.Date(2026, 2, 8, 10, 0, 0, 0, time.UTC),
		Extensions: MessageExtensions{
			PlatformMessageID: "1001",
		},
	}
}

func mustUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	return id.String()
}
