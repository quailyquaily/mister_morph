package mixin

import (
	"context"
	"fmt"
	"strings"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

type SendTextOptions struct {
	MessageID      string
	QuoteMessageID string
}

type DeliveryTarget struct {
	ConversationID string
	RecipientID    string
}

type SendTextFunc func(context.Context, DeliveryTarget, string, SendTextOptions) error

type DeliveryAdapterOptions struct {
	SendText SendTextFunc
}

type DeliveryAdapter struct {
	sendText SendTextFunc
}

func NewDeliveryAdapter(opts DeliveryAdapterOptions) (*DeliveryAdapter, error) {
	if opts.SendText == nil {
		return nil, fmt.Errorf("send text func is required")
	}
	return &DeliveryAdapter{sendText: opts.SendText}, nil
}

func (a *DeliveryAdapter) Deliver(ctx context.Context, msg busruntime.BusMessage) (bool, bool, error) {
	if a == nil || a.sendText == nil {
		return false, false, fmt.Errorf("mixin delivery adapter is not initialized")
	}
	if ctx == nil {
		return false, false, fmt.Errorf("context is required")
	}
	if msg.Direction != busruntime.DirectionOutbound {
		return false, false, fmt.Errorf("direction must be outbound")
	}
	if msg.Channel != busruntime.ChannelMixin {
		return false, false, fmt.Errorf("channel must be mixin")
	}
	conversationID, err := busruntime.ParseMixinConversationKey(msg.ConversationKey)
	if err != nil {
		return false, false, err
	}
	envelope, err := msg.Envelope()
	if err != nil {
		return false, false, err
	}
	text := strings.TrimSpace(envelope.Text)
	if text == "" {
		return false, false, fmt.Errorf("mixin outbound text is empty")
	}
	quoteMessageID := strings.TrimSpace(msg.Extensions.ReplyTo)
	if quoteMessageID == "" {
		quoteMessageID = strings.TrimSpace(envelope.ReplyTo)
	}
	if err := a.sendText(ctx, DeliveryTarget{ConversationID: conversationID, RecipientID: strings.TrimSpace(msg.ParticipantKey)}, text, SendTextOptions{MessageID: strings.TrimSpace(envelope.MessageID), QuoteMessageID: quoteMessageID}); err != nil {
		return false, false, err
	}
	return true, false, nil
}
