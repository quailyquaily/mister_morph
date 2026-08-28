package mixin

import (
	"context"
	"testing"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

func TestDeliveryAdapterSendsMixinText(t *testing.T) {
	var gotTarget DeliveryTarget
	var gotText string
	var gotOptions SendTextOptions
	adapter, err := NewDeliveryAdapter(DeliveryAdapterOptions{
		SendText: func(_ context.Context, target DeliveryTarget, text string, opts SendTextOptions) error {
			gotTarget, gotText, gotOptions = target, text, opts
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conversationKey, err := busruntime.BuildMixinConversationKey("8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: "7ea3356d-3c57-4ef5-a49c-71f2ba7cb312",
		Text:      "hello",
		SentAt:    "2026-08-27T10:00:00Z",
		SessionID: "01990a6b-f2e0-7d6c-9df2-71cff7241816",
		ReplyTo:   "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted, deduped, err := adapter.Deliver(context.Background(), busruntime.BusMessage{
		Direction:       busruntime.DirectionOutbound,
		Channel:         busruntime.ChannelMixin,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		ParticipantKey:  "773e5e77-4107-45c2-b648-8fc722ed77f5",
		IdempotencyKey:  "mixin:test",
		PayloadBase64:   payload,
		Extensions: busruntime.MessageExtensions{
			ReplyTo: "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
		},
	})
	if err != nil || !accepted || deduped {
		t.Fatalf("Deliver() accepted=%v deduped=%v err=%v", accepted, deduped, err)
	}
	if gotTarget.ConversationID != "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34" || gotTarget.RecipientID != "773e5e77-4107-45c2-b648-8fc722ed77f5" || gotText != "hello" || gotOptions.MessageID != "7ea3356d-3c57-4ef5-a49c-71f2ba7cb312" || gotOptions.QuoteMessageID != "a4ec1e53-f147-439a-82cd-2e5e4a95a152" {
		t.Fatalf("send = %#v %q %#v", gotTarget, gotText, gotOptions)
	}
}
