package lark

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

func TestInboundAdapterHandleInboundMessage(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bus, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 4, Logger: logger})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer bus.Close()

	store := contacts.NewFileStore(t.TempDir())
	adapter, err := NewInboundAdapter(InboundAdapterOptions{
		Bus:   bus,
		Store: store,
		Now: func() time.Time {
			return time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewInboundAdapter() error = %v", err)
	}

	delivered := make(chan busruntime.BusMessage, 1)
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(ctx context.Context, msg busruntime.BusMessage) error {
		delivered <- msg
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	accepted, err := adapter.HandleInboundMessage(context.Background(), InboundMessage{
		ChatID:       "oc_group123",
		MessageID:    "om_1001",
		ChatType:     "group",
		FromUserID:   "ou_123",
		DisplayName:  "Alice Lark",
		Text:         "hello lark",
		MentionUsers: []string{"ou_123", "ou_456"},
		EventID:      "ev_001",
		ImagePaths:   []string{"/tmp/a.png", "/tmp/a.png", "/tmp/b.jpg"},
		ImageKeys:    []string{"img_123", "img_123", "img_456"},
		SentAt:       time.Date(2026, 3, 6, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleInboundMessage() error = %v", err)
	}
	if !accepted {
		t.Fatalf("HandleInboundMessage() accepted=false, want true")
	}

	select {
	case msg := <-delivered:
		if msg.Channel != busruntime.ChannelLark {
			t.Fatalf("channel mismatch: got %s want %s", msg.Channel, busruntime.ChannelLark)
		}
		if msg.ConversationKey != "lark:oc_group123" {
			t.Fatalf("conversation_key mismatch: got %q want %q", msg.ConversationKey, "lark:oc_group123")
		}
		if msg.ParticipantKey != "ou_123" {
			t.Fatalf("participant_key mismatch: got %q want %q", msg.ParticipantKey, "ou_123")
		}
		if msg.Extensions.ReplyTo != "om_1001" {
			t.Fatalf("reply_to mismatch: got %q want %q", msg.Extensions.ReplyTo, "om_1001")
		}
		if msg.Extensions.PlatformMessageID != "oc_group123:om_1001" {
			t.Fatalf("platform_message_id mismatch: got %q", msg.Extensions.PlatformMessageID)
		}
		if len(msg.Extensions.ImagePaths) != 2 {
			t.Fatalf("image_paths len = %d, want 2", len(msg.Extensions.ImagePaths))
		}
		if len(msg.Extensions.ImageKeys) != 2 {
			t.Fatalf("image_keys len = %d, want 2", len(msg.Extensions.ImageKeys))
		}
		env, envErr := msg.Envelope()
		if envErr != nil {
			t.Fatalf("Envelope() error = %v", envErr)
		}
		if env.ReplyTo != "om_1001" {
			t.Fatalf("envelope reply_to mismatch: got %q want %q", env.ReplyTo, "om_1001")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("message not delivered")
	}

	accepted, err = adapter.HandleInboundMessage(context.Background(), InboundMessage{
		ChatID:     "oc_group123",
		MessageID:  "om_1001",
		ChatType:   "group",
		FromUserID: "ou_123",
		Text:       "hello lark",
	})
	if err != nil {
		t.Fatalf("HandleInboundMessage(second) error = %v", err)
	}
	if accepted {
		t.Fatalf("HandleInboundMessage(second) accepted=true, want false")
	}
}

func TestInboundMessageFromBusMessage(t *testing.T) {
	t.Parallel()

	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "lark:oc_group123:om_1001",
		Text:      "hello lark",
		SentAt:    "2026-03-06T00:00:00Z",
		SessionID: "0194e9d5-2f8f-7000-8000-000000000001",
		ReplyTo:   "om_1001",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	msg := busruntime.BusMessage{
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelLark,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: "lark:oc_group123",
		ParticipantKey:  "ou_123",
		IdempotencyKey:  "msg:lark_oc_group123_om_1001",
		CorrelationID:   "lark:oc_group123:om_1001",
		PayloadBase64:   payloadBase64,
		CreatedAt:       time.Now().UTC(),
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: "oc_group123:om_1001",
			ReplyTo:           "om_1001",
			ChatType:          "group",
			FromUserRef:       "ou_123",
			FromDisplayName:   "Alice Lark",
			ChannelID:         "oc_group123",
			EventID:           "ev_001",
			MentionUsers:      []string{"ou_123", "ou_456"},
			ImagePaths:        []string{"/tmp/a.png", "/tmp/b.jpg"},
			ImageKeys:         []string{"img_123", "img_456"},
		},
	}

	inbound, err := InboundMessageFromBusMessage(msg)
	if err != nil {
		t.Fatalf("InboundMessageFromBusMessage() error = %v", err)
	}
	if inbound.ChatID != "oc_group123" {
		t.Fatalf("chat_id mismatch: got %q want %q", inbound.ChatID, "oc_group123")
	}
	if inbound.MessageID != "om_1001" {
		t.Fatalf("message_id mismatch: got %q want %q", inbound.MessageID, "om_1001")
	}
	if inbound.ChatType != "group" {
		t.Fatalf("chat_type mismatch: got %q want %q", inbound.ChatType, "group")
	}
	if inbound.FromUserID != "ou_123" {
		t.Fatalf("from_user_id mismatch: got %q want %q", inbound.FromUserID, "ou_123")
	}
	if inbound.Text != "hello lark" {
		t.Fatalf("text mismatch: got %q want %q", inbound.Text, "hello lark")
	}
	if len(inbound.ImagePaths) != 2 {
		t.Fatalf("image_paths len = %d, want 2", len(inbound.ImagePaths))
	}
	if len(inbound.ImageKeys) != 2 {
		t.Fatalf("image_keys len = %d, want 2", len(inbound.ImageKeys))
	}
}
