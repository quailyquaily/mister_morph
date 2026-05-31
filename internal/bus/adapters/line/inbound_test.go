package line

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
			return time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
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
		ChatID:       "Cgroup123",
		MessageID:    "m_1001",
		ReplyToken:   "rtok_abc",
		ChatType:     "group",
		FromUserID:   "U123",
		FromUsername: "alice",
		DisplayName:  "Alice W",
		Text:         "hello line",
		MentionUsers: []string{"alice", "bob"},
		ImagePaths:   []string{"/tmp/p1.jpg", "/tmp/p2.png"},
		ImagePending: true,
		EventID:      "ev_001",
	})
	if err != nil {
		t.Fatalf("HandleInboundMessage() error = %v", err)
	}
	if !accepted {
		t.Fatalf("HandleInboundMessage() accepted=false, want true")
	}

	select {
	case msg := <-delivered:
		if msg.Channel != busruntime.ChannelLine {
			t.Fatalf("channel mismatch: got %s want %s", msg.Channel, busruntime.ChannelLine)
		}
		if msg.ConversationKey != "line:Cgroup123" {
			t.Fatalf("conversation_key mismatch: got %q want %q", msg.ConversationKey, "line:Cgroup123")
		}
		if msg.ParticipantKey != "U123" {
			t.Fatalf("participant_key mismatch: got %q want %q", msg.ParticipantKey, "U123")
		}
		if msg.Extensions.ChatType != "group" {
			t.Fatalf("chat_type mismatch: got %q want %q", msg.Extensions.ChatType, "group")
		}
		if msg.Extensions.ReplyTo != "rtok_abc" {
			t.Fatalf("reply_to mismatch: got %q want %q", msg.Extensions.ReplyTo, "rtok_abc")
		}
		if !msg.Extensions.ImagePending {
			t.Fatalf("image_pending mismatch: got false want true")
		}
		if len(msg.Extensions.ImageAttachments) != 2 {
			t.Fatalf("image_attachments length mismatch: got %d want 2", len(msg.Extensions.ImageAttachments))
		}
		env, envErr := msg.Envelope()
		if envErr != nil {
			t.Fatalf("Envelope() error = %v", envErr)
		}
		if env.ReplyTo != "rtok_abc" {
			t.Fatalf("envelope reply_to mismatch: got %q want %q", env.ReplyTo, "rtok_abc")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("message not delivered")
	}

	accepted, err = adapter.HandleInboundMessage(context.Background(), InboundMessage{
		ChatID:     "Cgroup123",
		MessageID:  "m_1001",
		ChatType:   "group",
		FromUserID: "U123",
		Text:       "hello line",
	})
	if err != nil {
		t.Fatalf("HandleInboundMessage(second) error = %v", err)
	}
	if accepted {
		t.Fatalf("HandleInboundMessage(second) accepted=true, want false")
	}
}

func TestInboundAdapterRejectsUnsupportedChatType(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bus, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 4, Logger: logger})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer bus.Close()

	store := contacts.NewFileStore(t.TempDir())
	adapter, err := NewInboundAdapter(InboundAdapterOptions{Bus: bus, Store: store})
	if err != nil {
		t.Fatalf("NewInboundAdapter() error = %v", err)
	}

	_, err = adapter.HandleInboundMessage(context.Background(), InboundMessage{
		ChatID:     "Rroom123",
		MessageID:  "m_1002",
		ChatType:   "room",
		FromUserID: "U123",
		Text:       "hello",
	})
	if err == nil {
		t.Fatalf("expected unsupported chat_type error")
	}
}

func TestInboundMessageFromBusMessage(t *testing.T) {
	t.Parallel()

	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "line:Cgroup123:m_1001",
		Text:      "hello line",
		SentAt:    "2026-03-04T00:00:00Z",
		SessionID: "0194e9d5-2f8f-7000-8000-000000000001",
		ReplyTo:   "rtok_abc",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	conversationKey, err := busruntime.BuildLineConversationKey("Cgroup123")
	if err != nil {
		t.Fatalf("BuildLineConversationKey() error = %v", err)
	}
	msg := busruntime.BusMessage{
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelLine,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		ParticipantKey:  "U123",
		IdempotencyKey:  "msg:line_Cgroup123_m_1001",
		CorrelationID:   "line:Cgroup123:m_1001",
		PayloadBase64:   payloadBase64,
		CreatedAt:       time.Now().UTC(),
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: "Cgroup123:m_1001",
			ReplyTo:           "rtok_abc",
			ChatType:          "group",
			FromUserRef:       "U123",
			FromUsername:      "alice",
			FromDisplayName:   "Alice W",
			ChannelID:         "Cgroup123",
			EventID:           "ev_001",
			MentionUsers:      []string{"alice", "bob"},
			ImagePaths:        []string{"/tmp/p1.jpg", "/tmp/p2.jpg"},
			ImagePending:      true,
		},
	}

	inbound, err := InboundMessageFromBusMessage(msg)
	if err != nil {
		t.Fatalf("InboundMessageFromBusMessage() error = %v", err)
	}
	if inbound.ChatID != "Cgroup123" {
		t.Fatalf("chat_id mismatch: got %q want %q", inbound.ChatID, "Cgroup123")
	}
	if inbound.MessageID != "m_1001" {
		t.Fatalf("message_id mismatch: got %q want %q", inbound.MessageID, "m_1001")
	}
	if inbound.ChatType != "group" {
		t.Fatalf("chat_type mismatch: got %q want %q", inbound.ChatType, "group")
	}
	if inbound.FromUserID != "U123" {
		t.Fatalf("from_user_id mismatch: got %q want %q", inbound.FromUserID, "U123")
	}
	if inbound.Text != "hello line" {
		t.Fatalf("text mismatch: got %q want %q", inbound.Text, "hello line")
	}
	if len(inbound.ImagePaths) != 2 {
		t.Fatalf("image_paths length mismatch: got %d want 2", len(inbound.ImagePaths))
	}
	if len(inbound.ImageAttachments) != 2 {
		t.Fatalf("image_attachments length mismatch: got %d want 2", len(inbound.ImageAttachments))
	}
	if !inbound.ImagePending {
		t.Fatalf("image_pending mismatch: got false want true")
	}
}

func TestInboundAdapterHandleInboundPrivateMessage(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bus, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 4, Logger: logger})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer bus.Close()

	store := contacts.NewFileStore(t.TempDir())
	adapter, err := NewInboundAdapter(InboundAdapterOptions{Bus: bus, Store: store})
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
		ChatID:     "Uprivate001",
		MessageID:  "m_2001",
		ChatType:   "private",
		FromUserID: "Uprivate001",
		Text:       "hello private",
	})
	if err != nil {
		t.Fatalf("HandleInboundMessage() error = %v", err)
	}
	if !accepted {
		t.Fatalf("HandleInboundMessage() accepted=false, want true")
	}

	select {
	case msg := <-delivered:
		if msg.ConversationKey != "line:Uprivate001" {
			t.Fatalf("conversation_key mismatch: got %q want %q", msg.ConversationKey, "line:Uprivate001")
		}
		if msg.Extensions.ChatType != "private" {
			t.Fatalf("chat_type mismatch: got %q want %q", msg.Extensions.ChatType, "private")
		}
		if msg.ParticipantKey != "Uprivate001" {
			t.Fatalf("participant_key mismatch: got %q want %q", msg.ParticipantKey, "Uprivate001")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("message not delivered")
	}
}
