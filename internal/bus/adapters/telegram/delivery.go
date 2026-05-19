package telegram

import (
	"context"
	"fmt"
	"strings"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

type SendTextFunc func(ctx context.Context, target any, text string, opts SendTextOptions) error

type SendTextOptions struct {
	ReplyTo         string
	MessageThreadID int64
	CorrelationID   string
}

type DeliveryTarget struct {
	ChatID          int64
	MessageThreadID int64
}

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
		return false, false, fmt.Errorf("telegram delivery adapter is not initialized")
	}
	if ctx == nil {
		return false, false, fmt.Errorf("context is required")
	}
	if msg.Direction != busruntime.DirectionOutbound {
		return false, false, fmt.Errorf("direction must be outbound")
	}
	if msg.Channel != busruntime.ChannelTelegram {
		return false, false, fmt.Errorf("channel must be telegram")
	}
	target, err := targetFromMessage(msg)
	if err != nil {
		return false, false, err
	}
	env, err := msg.Envelope()
	if err != nil {
		return false, false, err
	}
	text := strings.TrimSpace(env.Text)
	replyTo := strings.TrimSpace(msg.Extensions.ReplyTo)
	if replyTo == "" {
		replyTo = strings.TrimSpace(env.ReplyTo)
	}
	if err := a.sendText(ctx, target, text, SendTextOptions{
		ReplyTo:         replyTo,
		MessageThreadID: msg.Extensions.MessageThreadID,
		CorrelationID:   strings.TrimSpace(msg.CorrelationID),
	}); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func telegramPartsFromConversationKey(conversationKey string) (int64, int64, error) {
	chatID, messageThreadID, err := busruntime.ParseTelegramConversationKey(conversationKey)
	if err != nil {
		return 0, 0, err
	}
	return chatID, messageThreadID, nil
}

func targetFromMessage(msg busruntime.BusMessage) (any, error) {
	chatID, keyMessageThreadID, err := telegramPartsFromConversationKey(msg.ConversationKey)
	if err != nil {
		return nil, fmt.Errorf("telegram conversation key is invalid")
	}
	messageThreadID := msg.Extensions.MessageThreadID
	if messageThreadID <= 0 {
		messageThreadID = keyMessageThreadID
	}
	return DeliveryTarget{ChatID: chatID, MessageThreadID: messageThreadID}, nil
}
