package slack

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

func TestSlackOutboundEventFromBusMessage(t *testing.T) {
	sessionID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	payload, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "msg_1",
		Text:      "hello",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		SessionID: sessionID.String(),
		ReplyTo:   "1700000000.000100",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	event, err := slackOutboundEventFromBusMessage(busruntime.BusMessage{
		ConversationKey: "slack:T111:C222",
		Topic:           busruntime.TopicChatMessage,
		PayloadBase64:   payload,
		CorrelationID:   "slack:error:C222:1700000000.000100",
		Extensions: busruntime.MessageExtensions{
			TeamID:    "T111",
			ChannelID: "C222",
			ThreadTS:  "1700000000.000100",
		},
	})
	if err != nil {
		t.Fatalf("slackOutboundEventFromBusMessage() error = %v", err)
	}
	if event.ConversationKey != "slack:T111:C222" {
		t.Fatalf("conversation key = %q, want slack:T111:C222", event.ConversationKey)
	}
	if event.TeamID != "T111" || event.ChannelID != "C222" {
		t.Fatalf("team/channel = %q/%q, want T111/C222", event.TeamID, event.ChannelID)
	}
	if event.ThreadTS != "1700000000.000100" {
		t.Fatalf("thread ts = %q, want 1700000000.000100", event.ThreadTS)
	}
	if event.Text != "hello" {
		t.Fatalf("text = %q, want hello", event.Text)
	}
	if event.Kind != "error" {
		t.Fatalf("kind = %q, want error", event.Kind)
	}
}

func TestSlackOutboundKind(t *testing.T) {
	if got := slackOutboundKind("slack:plan:C:1"); got != "plan_progress" {
		t.Fatalf("kind(plan) = %q, want plan_progress", got)
	}
	if got := slackOutboundKind("slack:error:C:1"); got != "error" {
		t.Fatalf("kind(error) = %q, want error", got)
	}
	if got := slackOutboundKind("slack:message:C:1"); got != "message" {
		t.Fatalf("kind(message) = %q, want message", got)
	}
}

func TestPublishSlackBusOutboundPreservesThreadTS(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: 32,
		Logger:      logger,
		Component:   "slack-test",
	})
	if err != nil {
		t.Fatalf("StartInproc() error = %v", err)
	}
	defer inprocBus.Close()

	gotCh := make(chan busruntime.BusMessage, 1)
	if err := inprocBus.Subscribe(busruntime.TopicChatMessage, func(ctx context.Context, msg busruntime.BusMessage) error {
		select {
		case gotCh <- msg:
		default:
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	const threadTS = "1700000000.000100"
	_, err = publishSlackBusOutbound(context.Background(), inprocBus, "T111", "C222", "hello", threadTS, "corr:test")
	if err != nil {
		t.Fatalf("publishSlackBusOutbound() error = %v", err)
	}

	select {
	case msg := <-gotCh:
		if msg.ConversationKey != "slack:T111:C222" {
			t.Fatalf("conversation_key = %q, want %q", msg.ConversationKey, "slack:T111:C222")
		}
		if strings.TrimSpace(msg.Extensions.ThreadTS) != threadTS {
			t.Fatalf("extensions.thread_ts = %q, want %q", msg.Extensions.ThreadTS, threadTS)
		}
		if strings.TrimSpace(msg.Extensions.ReplyTo) != threadTS {
			t.Fatalf("extensions.reply_to = %q, want %q", msg.Extensions.ReplyTo, threadTS)
		}
		env, err := msg.Envelope()
		if err != nil {
			t.Fatalf("Envelope() error = %v", err)
		}
		if strings.TrimSpace(env.ReplyTo) != threadTS {
			t.Fatalf("envelope.reply_to = %q, want %q", env.ReplyTo, threadTS)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("did not receive outbound bus message")
	}
}
