package telegram

import (
	"context"
	"io"
	"log/slog"
	"strings"
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
			return time.Date(2026, 2, 8, 0, 0, 0, 0, time.UTC)
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
		ChatID:           12345,
		MessageThreadID:  901,
		MessageID:        678,
		ReplyToMessageID: 677,
		ChatType:         "private",
		FromUserID:       777,
		FromUsername:     "alice",
		FromFirstName:    "Alice",
		FromLastName:     "W",
		FromDisplayName:  "Alice W",
		FromIsAgent:      true,
		Text:             "hello",
		MentionUsers:     []string{"alice", "bob"},
		MentionParticipants: []busruntime.MessageParticipant{
			{ID: "42", Nickname: "No Handle"},
		},
		ImagePaths: []string{"/tmp/p1.jpg", "/tmp/p2.png"},
	})
	if err != nil {
		t.Fatalf("HandleInboundMessage() error = %v", err)
	}
	if !accepted {
		t.Fatalf("HandleInboundMessage() accepted=false, want true")
	}

	select {
	case msg := <-delivered:
		if msg.Channel != busruntime.ChannelTelegram {
			t.Fatalf("channel mismatch: got %s want %s", msg.Channel, busruntime.ChannelTelegram)
		}
		if msg.Extensions.ChatType != "private" {
			t.Fatalf("chat_type mismatch: got %q want %q", msg.Extensions.ChatType, "private")
		}
		if msg.ConversationKey != "tg:12345_901" {
			t.Fatalf("conversation_key mismatch: got %q want %q", msg.ConversationKey, "tg:12345_901")
		}
		if msg.Extensions.MessageThreadID != 901 {
			t.Fatalf("message_thread_id mismatch: got %d want 901", msg.Extensions.MessageThreadID)
		}
		if msg.Extensions.FromUserID != 777 {
			t.Fatalf("from_user_id mismatch: got %d want 777", msg.Extensions.FromUserID)
		}
		if !msg.Extensions.FromIsAgent {
			t.Fatal("from_is_agent mismatch: got false want true")
		}
		if len(msg.Extensions.MentionParticipants) != 1 || msg.Extensions.MentionParticipants[0].ID != "42" {
			t.Fatalf("mention_participants mismatch: %#v", msg.Extensions.MentionParticipants)
		}
		if msg.Extensions.ReplyTo != "677" {
			t.Fatalf("reply_to mismatch: got %q want %q", msg.Extensions.ReplyTo, "677")
		}
		if len(msg.Extensions.ImagePaths) != 2 {
			t.Fatalf("image_paths length mismatch: got %d want 2", len(msg.Extensions.ImagePaths))
		}
		if len(msg.Extensions.ImageAttachments) != 2 {
			t.Fatalf("image_attachments length mismatch: got %d want 2", len(msg.Extensions.ImageAttachments))
		}
		if msg.Extensions.ImageAttachments[0].Path != "/tmp/p1.jpg" || msg.Extensions.ImageAttachments[1].Path != "/tmp/p2.png" {
			t.Fatalf("image_attachments mismatch: %#v", msg.Extensions.ImageAttachments)
		}
		env, envErr := msg.Envelope()
		if envErr != nil {
			t.Fatalf("Envelope() error = %v", envErr)
		}
		if env.ReplyTo != "677" {
			t.Fatalf("envelope reply_to mismatch: got %q want %q", env.ReplyTo, "677")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("message not delivered")
	}

	accepted, err = adapter.HandleInboundMessage(context.Background(), InboundMessage{
		ChatID:          12345,
		MessageID:       678,
		ChatType:        "private",
		FromUserID:      777,
		FromUsername:    "alice",
		FromFirstName:   "Alice",
		FromLastName:    "W",
		FromDisplayName: "Alice W",
		Text:            "hello",
		MentionUsers:    []string{"alice", "bob"},
		ImagePaths:      []string{"/tmp/p1.jpg"},
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
		MessageID: "telegram:12345:678",
		Text:      "hello",
		SentAt:    "2026-02-08T00:00:00Z",
		SessionID: "0194e9d5-2f8f-7000-8000-000000000001",
		ReplyTo:   "777",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	conversationKey, err := busruntime.BuildTelegramTopicConversationKey("12345", 901)
	if err != nil {
		t.Fatalf("BuildTelegramTopicConversationKey() error = %v", err)
	}
	msg := busruntime.BusMessage{
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelTelegram,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		IdempotencyKey:  "msg:telegram_12345_678",
		CorrelationID:   "telegram:12345:678",
		PayloadBase64:   payloadBase64,
		CreatedAt:       time.Now().UTC(),
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: "12345:678",
			ReplyTo:           "777",
			ChatType:          "group",
			FromUserID:        9001,
			FromUsername:      "neo",
			FromIsAgent:       true,
			MentionUsers:      []string{"neo", "morpheus"},
			MentionParticipants: []busruntime.MessageParticipant{
				{ID: "42", Nickname: "No Handle"},
			},
			ImagePaths: []string{"/tmp/p1.jpg", "/tmp/p2.jpg"},
		},
	}
	inbound, err := InboundMessageFromBusMessage(msg)
	if err != nil {
		t.Fatalf("InboundMessageFromBusMessage() error = %v", err)
	}
	if inbound.ChatID != 12345 {
		t.Fatalf("chat_id mismatch: got %d want 12345", inbound.ChatID)
	}
	if inbound.MessageThreadID != 901 {
		t.Fatalf("message_thread_id mismatch: got %d want 901", inbound.MessageThreadID)
	}
	if inbound.MessageID != 678 {
		t.Fatalf("message_id mismatch: got %d want 678", inbound.MessageID)
	}
	if inbound.ReplyToMessageID != 777 {
		t.Fatalf("reply_to_message_id mismatch: got %d want 777", inbound.ReplyToMessageID)
	}
	if inbound.ChatType != "group" {
		t.Fatalf("chat_type mismatch: got %q want %q", inbound.ChatType, "group")
	}
	if inbound.FromUserID != 9001 {
		t.Fatalf("from_user_id mismatch: got %d want 9001", inbound.FromUserID)
	}
	if !inbound.FromIsAgent {
		t.Fatal("from_is_agent mismatch: got false want true")
	}
	if len(inbound.MentionParticipants) != 1 || inbound.MentionParticipants[0].Nickname != "No Handle" {
		t.Fatalf("mention_participants mismatch: %#v", inbound.MentionParticipants)
	}
	if inbound.Text != "hello" {
		t.Fatalf("text mismatch: got %q want %q", inbound.Text, "hello")
	}
	if len(inbound.ImagePaths) != 2 {
		t.Fatalf("image_paths length mismatch: got %d want 2", len(inbound.ImagePaths))
	}
	if len(inbound.ImageAttachments) != 2 {
		t.Fatalf("image_attachments length mismatch: got %d want 2", len(inbound.ImageAttachments))
	}
}

func TestInboundMessageFromBusMessageRejectsMessageThreadIDConflict(t *testing.T) {
	t.Parallel()

	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "telegram:12345:678",
		Text:      "hello",
		SentAt:    "2026-02-08T00:00:00Z",
		SessionID: "0194e9d5-2f8f-7000-8000-000000000001",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	msg := busruntime.BusMessage{
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelTelegram,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: "tg:12345_901",
		IdempotencyKey:  "msg:telegram_12345_678",
		CorrelationID:   "telegram:12345:678",
		PayloadBase64:   payloadBase64,
		CreatedAt:       time.Now().UTC(),
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: "12345:678",
			ChatType:          "group",
			MessageThreadID:   902,
		},
	}
	_, err = InboundMessageFromBusMessage(msg)
	if err == nil {
		t.Fatalf("InboundMessageFromBusMessage() expected conflict error")
	}
	if !strings.Contains(err.Error(), "conflicts with conversation_key") {
		t.Fatalf("InboundMessageFromBusMessage() error mismatch: got %q", err.Error())
	}
}
