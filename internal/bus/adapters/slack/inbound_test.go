package slack

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
			return time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC)
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
		TeamID:       "T111",
		ChannelID:    "C222",
		ChatType:     "channel",
		MessageTS:    "1739667600.000100",
		ThreadTS:     "1739667000.000050",
		UserID:       "U333",
		Username:     "alice",
		DisplayName:  "Alice W",
		Text:         "hello from slack",
		MentionUsers: []string{"@alice", "@bob"},
		EventID:      "Ev01",
		ImagePaths:   []string{"/tmp/a.png", "/tmp/a.png", "/tmp/b.jpg"},
	})
	if err != nil {
		t.Fatalf("HandleInboundMessage() error = %v", err)
	}
	if !accepted {
		t.Fatalf("HandleInboundMessage() accepted=false, want true")
	}

	select {
	case msg := <-delivered:
		if msg.Channel != busruntime.ChannelSlack {
			t.Fatalf("channel mismatch: got %s want %s", msg.Channel, busruntime.ChannelSlack)
		}
		if msg.ConversationKey != "slack:T111:C222" {
			t.Fatalf("conversation_key mismatch: got %q want %q", msg.ConversationKey, "slack:T111:C222")
		}
		if msg.ParticipantKey != "T111:U333" {
			t.Fatalf("participant_key mismatch: got %q want %q", msg.ParticipantKey, "T111:U333")
		}
		if msg.Extensions.TeamID != "T111" {
			t.Fatalf("team_id mismatch: got %q want %q", msg.Extensions.TeamID, "T111")
		}
		if msg.Extensions.ChannelID != "C222" {
			t.Fatalf("channel_id mismatch: got %q want %q", msg.Extensions.ChannelID, "C222")
		}
		if msg.Extensions.FromUserRef != "U333" {
			t.Fatalf("from_user_ref mismatch: got %q want %q", msg.Extensions.FromUserRef, "U333")
		}
		if msg.Extensions.ThreadTS != "1739667000.000050" {
			t.Fatalf("thread_ts mismatch: got %q want %q", msg.Extensions.ThreadTS, "1739667000.000050")
		}
		if msg.Extensions.EventID != "Ev01" {
			t.Fatalf("event_id mismatch: got %q want %q", msg.Extensions.EventID, "Ev01")
		}
		if len(msg.Extensions.ImagePaths) != 2 {
			t.Fatalf("image_paths len = %d, want 2", len(msg.Extensions.ImagePaths))
		}
		env, envErr := msg.Envelope()
		if envErr != nil {
			t.Fatalf("Envelope() error = %v", envErr)
		}
		if env.ReplyTo != "1739667000.000050" {
			t.Fatalf("envelope reply_to mismatch: got %q want %q", env.ReplyTo, "1739667000.000050")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("message not delivered")
	}

	accepted, err = adapter.HandleInboundMessage(context.Background(), InboundMessage{
		TeamID:      "T111",
		ChannelID:   "C222",
		ChatType:    "channel",
		MessageTS:   "1739667600.000100",
		UserID:      "U333",
		Username:    "alice",
		DisplayName: "Alice W",
		Text:        "hello from slack",
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
		MessageID: "slack:T111:C222:1739667600.000100",
		Text:      "hello from slack",
		SentAt:    "2026-02-16T00:00:00Z",
		SessionID: "0194e9d5-2f8f-7000-8000-000000000001",
		ReplyTo:   "1739667000.000050",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	conversationKey, err := busruntime.BuildSlackChannelConversationKey("T111:C222")
	if err != nil {
		t.Fatalf("BuildSlackChannelConversationKey() error = %v", err)
	}
	msg := busruntime.BusMessage{
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelSlack,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		ParticipantKey:  "T111:U333",
		IdempotencyKey:  "msg:slack_T111_C222_1739667600_000100",
		CorrelationID:   "slack:T111:C222:1739667600.000100",
		PayloadBase64:   payloadBase64,
		CreatedAt:       time.Now().UTC(),
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: "T111:C222:1739667600.000100",
			ReplyTo:           "1739667000.000050",
			ChatType:          "channel",
			FromUserRef:       "U333",
			FromUsername:      "alice",
			FromDisplayName:   "Alice W",
			TeamID:            "T111",
			ChannelID:         "C222",
			ThreadTS:          "1739667000.000050",
			EventID:           "Ev01",
			MentionUsers:      []string{"@alice", "@bob"},
			ImagePaths:        []string{"/tmp/a.png", "/tmp/b.jpg"},
		},
	}
	inbound, err := InboundMessageFromBusMessage(msg)
	if err != nil {
		t.Fatalf("InboundMessageFromBusMessage() error = %v", err)
	}
	if inbound.TeamID != "T111" {
		t.Fatalf("team_id mismatch: got %q want %q", inbound.TeamID, "T111")
	}
	if inbound.ChannelID != "C222" {
		t.Fatalf("channel_id mismatch: got %q want %q", inbound.ChannelID, "C222")
	}
	if inbound.MessageTS != "1739667600.000100" {
		t.Fatalf("message_ts mismatch: got %q want %q", inbound.MessageTS, "1739667600.000100")
	}
	if inbound.ThreadTS != "1739667000.000050" {
		t.Fatalf("thread_ts mismatch: got %q want %q", inbound.ThreadTS, "1739667000.000050")
	}
	if inbound.UserID != "U333" {
		t.Fatalf("user_id mismatch: got %q want %q", inbound.UserID, "U333")
	}
	if inbound.Text != "hello from slack" {
		t.Fatalf("text mismatch: got %q want %q", inbound.Text, "hello from slack")
	}
	if len(inbound.ImagePaths) != 2 {
		t.Fatalf("image_paths len = %d, want 2", len(inbound.ImagePaths))
	}
}
