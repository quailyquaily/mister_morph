package telegram

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

func TestPublishTelegramBusOutbound(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bus, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 4, Logger: logger})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer bus.Close()

	got := make(chan busruntime.BusMessage, 1)
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(ctx context.Context, msg busruntime.BusMessage) error {
		got <- msg
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	messageID, err := publishTelegramBusOutbound(context.Background(), bus, 12345, 901, "hello", "", "corr:test")
	if err != nil {
		t.Fatalf("publishTelegramBusOutbound() error = %v", err)
	}
	if messageID == "" {
		t.Fatalf("message_id should not be empty")
	}

	select {
	case msg := <-got:
		if msg.Direction != busruntime.DirectionOutbound {
			t.Fatalf("direction mismatch: got %s want %s", msg.Direction, busruntime.DirectionOutbound)
		}
		if msg.Channel != busruntime.ChannelTelegram {
			t.Fatalf("channel mismatch: got %s want %s", msg.Channel, busruntime.ChannelTelegram)
		}
		if msg.Topic != busruntime.TopicChatMessage {
			t.Fatalf("topic mismatch: got %s want %s", msg.Topic, busruntime.TopicChatMessage)
		}
		if msg.ConversationKey != "tg:12345_901" {
			t.Fatalf("conversation_key mismatch: got %q want %q", msg.ConversationKey, "tg:12345_901")
		}
		if msg.Extensions.MessageThreadID != 0 {
			t.Fatalf("message_thread_id should not be duplicated in outbound extensions, got %d", msg.Extensions.MessageThreadID)
		}
		env, err := msg.Envelope()
		if err != nil {
			t.Fatalf("Envelope() error = %v", err)
		}
		if env.Text != "hello" {
			t.Fatalf("envelope text mismatch: got %q want %q", env.Text, "hello")
		}
		if env.SessionID == "" {
			t.Fatalf("session_id should not be empty")
		}
		if env.ReplyTo != "" {
			t.Fatalf("reply_to should be empty, got %q", env.ReplyTo)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("message not delivered")
	}
}

func TestPublishTelegramBusOutbound_WithReplyTo(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bus, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 4, Logger: logger})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer bus.Close()

	got := make(chan busruntime.BusMessage, 1)
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(ctx context.Context, msg busruntime.BusMessage) error {
		got <- msg
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	_, err = publishTelegramBusOutbound(context.Background(), bus, 12345, 0, "hello", "4321", "corr:test:reply")
	if err != nil {
		t.Fatalf("publishTelegramBusOutbound() error = %v", err)
	}

	select {
	case msg := <-got:
		env, envErr := msg.Envelope()
		if envErr != nil {
			t.Fatalf("Envelope() error = %v", envErr)
		}
		if env.ReplyTo != "4321" {
			t.Fatalf("envelope reply_to mismatch: got %q want %q", env.ReplyTo, "4321")
		}
		if msg.Extensions.ReplyTo != "4321" {
			t.Fatalf("extensions reply_to mismatch: got %q want %q", msg.Extensions.ReplyTo, "4321")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("message not delivered")
	}
}
