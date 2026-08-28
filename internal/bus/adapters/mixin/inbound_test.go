package mixin

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

func TestInboundAdapterPublishesMixinMessage(t *testing.T) {
	ctx := context.Background()
	store := contacts.NewFileStore(t.TempDir())
	if err := store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	bus, err := newTestBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	received := make(chan busruntime.BusMessage, 1)
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(_ context.Context, msg busruntime.BusMessage) error {
		received <- msg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewInboundAdapter(InboundAdapterOptions{Bus: bus, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	sentAt := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	accepted, err := adapter.HandleInboundMessage(ctx, InboundMessage{
		ConversationID: "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34",
		MessageID:      "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
		SentAt:         sentAt,
		ChatType:       "GROUP",
		FromUserID:     "773e5e77-4107-45c2-b648-8fc722ed77f5",
		IdentityNumber: "7000123456",
		DisplayName:    "Alice",
		Text:           "@7000999999 hello",
		QuoteMessageID: "da133b6c-48d1-4e8f-8e78-bb9e550d08f1",
		MentionUserIDs: []string{"94aa0961-9c12-4637-9a67-0c0798c24649"},
		ImageAttachments: []busruntime.ImageAttachment{{
			Path:               "/tmp/mixin-photo.png",
			SourceMessageID:    "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
			SourceAttachmentID: "66666666-6666-6666-6666-666666666666",
			MIMEType:           "image/png",
		}},
	})
	if err != nil || !accepted {
		t.Fatalf("HandleInboundMessage() accepted=%v error=%v", accepted, err)
	}
	select {
	case msg := <-received:
		if msg.Channel != busruntime.ChannelMixin || msg.ConversationKey != "mixin:8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34" {
			t.Fatalf("routing = %#v", msg)
		}
		if msg.Extensions.PlatformMessageID != "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34:a4ec1e53-f147-439a-82cd-2e5e4a95a152" {
			t.Fatalf("platform message id = %q", msg.Extensions.PlatformMessageID)
		}
		if msg.Extensions.FromUserRef != "773e5e77-4107-45c2-b648-8fc722ed77f5" || msg.Extensions.FromUsername != "7000123456" || msg.Extensions.ReplyTo != "da133b6c-48d1-4e8f-8e78-bb9e550d08f1" {
			t.Fatalf("extensions = %#v", msg.Extensions)
		}
		if len(msg.Extensions.ImageAttachments) != 1 || msg.Extensions.ImageAttachments[0].Path != "/tmp/mixin-photo.png" {
			t.Fatalf("image attachments = %#v", msg.Extensions.ImageAttachments)
		}
		envelope, err := msg.Envelope()
		if err != nil {
			t.Fatal(err)
		}
		if envelope.Text != "@7000999999 hello" || envelope.SentAt != sentAt.Format(time.RFC3339) {
			t.Fatalf("envelope = %#v", envelope)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus message")
	}
}

func TestInboundAdapterDedupesByConversationAndMessageID(t *testing.T) {
	ctx := context.Background()
	store := contacts.NewFileStore(t.TempDir())
	if err := store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	bus, err := newTestBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(context.Context, busruntime.BusMessage) error { return nil }); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewInboundAdapter(InboundAdapterOptions{Bus: bus, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	message := InboundMessage{
		ConversationID: "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34",
		MessageID:      "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
		ChatType:       "CONTACT",
		FromUserID:     "773e5e77-4107-45c2-b648-8fc722ed77f5",
		Text:           "hello",
	}
	accepted, err := adapter.HandleInboundMessage(ctx, message)
	if err != nil || !accepted {
		t.Fatalf("first accepted=%v err=%v", accepted, err)
	}
	accepted, err = adapter.HandleInboundMessage(ctx, message)
	if err != nil || accepted {
		t.Fatalf("second accepted=%v err=%v", accepted, err)
	}
}

func TestInboundMessageRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := contacts.NewFileStore(t.TempDir())
	if err := store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	bus, err := newTestBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	received := make(chan busruntime.BusMessage, 1)
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(_ context.Context, msg busruntime.BusMessage) error {
		received <- msg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewInboundAdapter(InboundAdapterOptions{Bus: bus, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	want := InboundMessage{
		ConversationID: "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34",
		MessageID:      "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
		ChatType:       "CONTACT",
		FromUserID:     "773e5e77-4107-45c2-b648-8fc722ed77f5",
		IdentityNumber: "7000123456",
		DisplayName:    "Alice",
		Text:           "hello",
		FromIsAgent:    true,
		ImageAttachments: []busruntime.ImageAttachment{{
			Path: "/tmp/mixin-photo.png", SourceAttachmentID: "66666666-6666-6666-6666-666666666666",
		}},
	}
	if _, err := adapter.HandleInboundMessage(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := InboundMessageFromBusMessage(<-received)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConversationID != want.ConversationID || got.MessageID != want.MessageID || got.FromUserID != want.FromUserID || got.IdentityNumber != want.IdentityNumber || got.Text != want.Text || !got.FromIsAgent || len(got.ImageAttachments) != 1 {
		t.Fatalf("round trip = %#v", got)
	}
}

func newTestBus() (*busruntime.Inproc, error) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 4, Logger: logger})
}
