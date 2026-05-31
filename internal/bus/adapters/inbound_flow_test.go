package adapters

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

func TestInboundFlowPublishValidatedInbound(t *testing.T) {
	ctx := context.Background()
	store := contacts.NewFileStore(filepath.Join(t.TempDir(), "contacts"))
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	b, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 8, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer b.Close()

	var (
		mu    sync.Mutex
		count int
		done  = make(chan struct{})
	)
	if err := b.Subscribe(busruntime.TopicChatMessage, func(ctx context.Context, msg busruntime.BusMessage) error {
		mu.Lock()
		count++
		if count == 1 {
			close(done)
		}
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	now := time.Date(2026, 2, 8, 23, 0, 0, 0, time.UTC)
	flow, err := NewInboundFlow(InboundFlowOptions{
		Bus:     b,
		Store:   store,
		Channel: "telegram",
		Now:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewInboundFlow() error = %v", err)
	}

	msg := validInboundMessage(t)
	accepted, err := flow.PublishValidatedInbound(ctx, "msg-1001", msg)
	if err != nil {
		t.Fatalf("PublishValidatedInbound(first) error = %v", err)
	}
	if !accepted {
		t.Fatalf("PublishValidatedInbound(first) accepted=false, want true")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for first delivery")
	}

	accepted, err = flow.PublishValidatedInbound(ctx, "msg-1001", msg)
	if err != nil {
		t.Fatalf("PublishValidatedInbound(second) error = %v", err)
	}
	if accepted {
		t.Fatalf("PublishValidatedInbound(second) accepted=true, want false")
	}

	record, ok, err := store.GetBusInboxRecord(ctx, contacts.ChannelTelegram, "msg-1001")
	if err != nil {
		t.Fatalf("GetBusInboxRecord() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetBusInboxRecord() expected ok=true")
	}
	if !record.SeenAt.Equal(now) {
		t.Fatalf("SeenAt mismatch: got %s want %s", record.SeenAt.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("delivery count mismatch: got %d want 1", count)
	}
}

func TestInboundFlowValidationBoundary(t *testing.T) {
	ctx := context.Background()
	store := contacts.NewFileStore(filepath.Join(t.TempDir(), "contacts"))
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	b, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 4, Logger: newTestLogger()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	defer b.Close()
	if err := b.Subscribe(busruntime.TopicChatMessage, func(ctx context.Context, msg busruntime.BusMessage) error { return nil }); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	flow, err := NewInboundFlow(InboundFlowOptions{
		Bus:     b,
		Store:   store,
		Channel: "telegram",
	})
	if err != nil {
		t.Fatalf("NewInboundFlow() error = %v", err)
	}

	msg := validInboundMessage(t)
	msg.IdempotencyKey = ""
	if _, err := flow.PublishValidatedInbound(ctx, "msg-1002", msg); err == nil {
		t.Fatalf("PublishValidatedInbound() expected validation error")
	}
	if _, ok, err := store.GetBusInboxRecord(ctx, contacts.ChannelTelegram, "msg-1002"); err != nil {
		t.Fatalf("GetBusInboxRecord() error = %v", err)
	} else if ok {
		t.Fatalf("inbox record should not be written on validation failure")
	}
}

func TestNormalizeImageAttachmentsTrimsAndDedupes(t *testing.T) {
	got, err := NormalizeImageAttachments([]busruntime.ImageAttachment{
		{
			Path:               " /tmp/a.png ",
			SourceMessageID:    " 100 ",
			SourceAttachmentID: " file_1 ",
			MIMEType:           " image/png ",
		},
		{
			Path:               "/tmp/a.png",
			SourceMessageID:    "100",
			SourceAttachmentID: "file_1",
			MIMEType:           "image/png",
		},
	})
	if err != nil {
		t.Fatalf("NormalizeImageAttachments() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("attachments len = %d, want 1", len(got))
	}
	if got[0].Path != "/tmp/a.png" || got[0].SourceMessageID != "100" || got[0].SourceAttachmentID != "file_1" || got[0].MIMEType != "image/png" {
		t.Fatalf("attachment mismatch: %#v", got[0])
	}
}

func TestNormalizeImageAttachmentsRequiresPath(t *testing.T) {
	if _, err := NormalizeImageAttachments([]busruntime.ImageAttachment{{SourceMessageID: "100"}}); err == nil {
		t.Fatalf("NormalizeImageAttachments() expected error")
	}
}

func validInboundMessage(t *testing.T) busruntime.BusMessage {
	t.Helper()
	sessionID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	conversationKey, err := busruntime.BuildTelegramChatConversationKey("1001")
	if err != nil {
		t.Fatalf("BuildTelegramChatConversationKey() error = %v", err)
	}
	payload, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "msg_" + uuid.NewString(),
		Text:      "hello",
		SentAt:    "2026-02-08T20:00:00Z",
		SessionID: sessionID.String(),
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	return busruntime.BusMessage{
		ID:              "bus_1",
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		IdempotencyKey:  fmt.Sprintf("msg:%s", "m_1001"),
		PayloadBase64:   payload,
		Channel:         busruntime.ChannelTelegram,
	}
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
