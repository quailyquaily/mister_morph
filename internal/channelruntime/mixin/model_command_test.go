package mixin

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	mixinbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/mixin"
)

func TestMixinIDAndResetCommands(t *testing.T) {
	bus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: 4, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Component: "mixin-command-test",
	})
	if err != nil {
		t.Fatalf("StartInproc() error = %v", err)
	}
	defer bus.Close()
	replies := make(chan string, 2)
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(_ context.Context, msg busruntime.BusMessage) error {
		if msg.Direction == busruntime.DirectionOutbound {
			envelope, envelopeErr := msg.Envelope()
			if envelopeErr != nil {
				return envelopeErr
			}
			replies <- envelope.Text
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	resetCalls := 0
	for _, test := range []struct {
		text string
		want string
	}{
		{text: "/id", want: "conversation_id=" + testConversationID + " type=CONTACT"},
		{text: "/reset", want: "ok (reset)"},
	} {
		handled, err := maybeHandleMixinCommand(
			context.Background(), Dependencies{}, bus, nil, "mixin:"+testConversationID,
			mixinbus.InboundMessage{ConversationID: testConversationID, FromUserID: testUserID, ChatType: "CONTACT", MessageID: test.text, Text: test.text},
			nil,
			func(context.Context) error {
				resetCalls++
				return nil
			},
		)
		if err != nil || !handled {
			t.Fatalf("maybeHandleMixinCommand(%q) handled=%v, error=%v", test.text, handled, err)
		}
		select {
		case reply := <-replies:
			if !strings.Contains(reply, test.want) {
				t.Fatalf("reply for %q = %q, want %q", test.text, reply, test.want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no reply for %q", test.text)
		}
	}
	if resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", resetCalls)
	}
}
