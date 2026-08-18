package slack

import (
	"context"
	"fmt"
	"strings"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
)

type SendTextFunc func(ctx context.Context, target any, text string, opts SendTextOptions) error

type SendTextOptions struct {
	ThreadTS string
}

type DeliveryTarget struct {
	TeamID    string
	ChannelID string
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
		return false, false, fmt.Errorf("slack delivery adapter is not initialized")
	}
	if ctx == nil {
		return false, false, fmt.Errorf("context is required")
	}
	if msg.Direction != busruntime.DirectionOutbound {
		return false, false, fmt.Errorf("direction must be outbound")
	}
	if msg.Channel != busruntime.ChannelSlack {
		return false, false, fmt.Errorf("channel must be slack")
	}
	teamID, channelID, err := slackConversationPartsFromKey(msg.ConversationKey)
	if err != nil {
		return false, false, err
	}
	target := DeliveryTarget{TeamID: teamID, ChannelID: channelID}
	env, err := msg.Envelope()
	if err != nil {
		return false, false, err
	}
	text := strings.TrimSpace(env.Text)
	threadTS := strings.TrimSpace(msg.Extensions.ThreadTS)
	if threadTS == "" {
		threadTS = strings.TrimSpace(msg.Extensions.ReplyTo)
	}
	if threadTS == "" {
		threadTS = strings.TrimSpace(env.ReplyTo)
	}
	if err := a.sendText(ctx, target, text, SendTextOptions{ThreadTS: threadTS}); err != nil {
		return false, false, err
	}
	return true, false, nil
}
