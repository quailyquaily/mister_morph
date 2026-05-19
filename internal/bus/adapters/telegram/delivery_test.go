package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

func TestDeliveryAdapterDeliver(t *testing.T) {
	t.Parallel()

	var gotChatID int64
	var gotMessageThreadID int64
	var gotText string
	var gotReplyTo string
	var gotCorrelationID string
	calls := 0
	adapter, err := NewDeliveryAdapter(DeliveryAdapterOptions{
		SendText: func(ctx context.Context, target any, text string, opts SendTextOptions) error {
			deliveryTarget, ok := target.(DeliveryTarget)
			if !ok {
				t.Fatalf("target type mismatch: got %T want DeliveryTarget", target)
			}
			gotChatID = deliveryTarget.ChatID
			gotMessageThreadID = deliveryTarget.MessageThreadID
			gotText = text
			gotReplyTo = opts.ReplyTo
			gotCorrelationID = opts.CorrelationID
			calls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewDeliveryAdapter() error = %v", err)
	}

	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "msg_4001",
		Text:      "hello telegram",
		SentAt:    "2026-02-08T00:00:00Z",
		SessionID: "0194e9d5-2f8f-7000-8000-000000000001",
		ReplyTo:   "98765",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	conversationKey, err := busruntime.BuildTelegramTopicConversationKey("12345", 901)
	if err != nil {
		t.Fatalf("BuildTelegramTopicConversationKey() error = %v", err)
	}
	msg := busruntime.BusMessage{
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelTelegram,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		IdempotencyKey:  "msg:msg_4001",
		CorrelationID:   "corr_3",
		PayloadBase64:   payloadBase64,
		CreatedAt:       time.Now().UTC(),
		Extensions: busruntime.MessageExtensions{
			MessageThreadID: 901,
		},
	}
	accepted, deduped, err := adapter.Deliver(context.Background(), msg)
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if !accepted {
		t.Fatalf("accepted mismatch: got %v want true", accepted)
	}
	if deduped {
		t.Fatalf("deduped mismatch: got %v want false", deduped)
	}
	if calls != 1 {
		t.Fatalf("send calls mismatch: got %d want 1", calls)
	}
	if gotChatID != 12345 {
		t.Fatalf("chat_id mismatch: got %d want 12345", gotChatID)
	}
	if gotMessageThreadID != 901 {
		t.Fatalf("message_thread_id mismatch: got %d want 901", gotMessageThreadID)
	}
	if gotText != "hello telegram" {
		t.Fatalf("text mismatch: got %q want %q", gotText, "hello telegram")
	}
	if gotReplyTo != "98765" {
		t.Fatalf("reply_to mismatch: got %q want %q", gotReplyTo, "98765")
	}
	if gotCorrelationID != "corr_3" {
		t.Fatalf("correlation_id mismatch: got %q want %q", gotCorrelationID, "corr_3")
	}
}

func TestDeliveryAdapterRejectsTelegramUsernameConversationKey(t *testing.T) {
	t.Parallel()

	calls := 0
	adapter, err := NewDeliveryAdapter(DeliveryAdapterOptions{
		SendText: func(ctx context.Context, target any, text string, opts SendTextOptions) error {
			calls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewDeliveryAdapter() error = %v", err)
	}

	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "msg_4002",
		Text:      "legacy username path",
		SentAt:    "2026-02-08T00:00:00Z",
		SessionID: "0194e9d5-2f8f-7000-8000-000000000002",
	})
	if err != nil {
		t.Fatalf("EncodeMessageEnvelope() error = %v", err)
	}
	msg := busruntime.BusMessage{
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelTelegram,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: "tg:@alice",
		ParticipantKey:  "@alice",
		IdempotencyKey:  "msg:msg_4002",
		CorrelationID:   "corr_4",
		PayloadBase64:   payloadBase64,
		CreatedAt:       time.Now().UTC(),
	}
	accepted, deduped, err := adapter.Deliver(context.Background(), msg)
	if err == nil {
		t.Fatalf("Deliver() expected error for tg:@ conversation key")
	}
	if !strings.Contains(err.Error(), "telegram conversation key is invalid") {
		t.Fatalf("Deliver() error mismatch: got %q", err.Error())
	}
	if accepted {
		t.Fatalf("accepted mismatch: got %v want false", accepted)
	}
	if deduped {
		t.Fatalf("deduped mismatch: got %v want false", deduped)
	}
	if calls != 0 {
		t.Fatalf("send calls mismatch: got %d want 0", calls)
	}
}
