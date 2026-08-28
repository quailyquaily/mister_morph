package adapters

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/channels"
)

type InboundStore interface {
	GetBusInboxRecord(ctx context.Context, channel string, platformMessageID string) (contacts.BusInboxRecord, bool, error)
	PutBusInboxRecord(ctx context.Context, record contacts.BusInboxRecord) error
}

type InboundFlowOptions struct {
	Bus     *busruntime.Inproc
	Store   InboundStore
	Channel string
	Now     func() time.Time
}

type InboundFlow struct {
	bus     *busruntime.Inproc
	store   InboundStore
	channel string
	nowFn   func() time.Time
}

func NewInboundFlow(opts InboundFlowOptions) (*InboundFlow, error) {
	if opts.Bus == nil {
		return nil, fmt.Errorf("bus is required")
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	channel := strings.ToLower(strings.TrimSpace(opts.Channel))
	switch channel {
	case channels.Telegram, channels.Slack, channels.Line, channels.Lark, channels.Discord, channels.Mixin:
	default:
		return nil, fmt.Errorf("unsupported channel: %q", opts.Channel)
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &InboundFlow{
		bus:     opts.Bus,
		store:   opts.Store,
		channel: channel,
		nowFn:   nowFn,
	}, nil
}

// PublishValidatedInbound applies the shared inbound path:
// validate+publish via bus, inbox dedupe by (channel, platform_message_id), and persist seen record.
func (f *InboundFlow) PublishValidatedInbound(ctx context.Context, platformMessageID string, msg busruntime.BusMessage) (bool, error) {
	return f.publishValidatedInbound(ctx, platformMessageID, msg, false)
}

// PublishValidatedInboundAndWait persists the seen record only after the bus
// subscriber has accepted the message.
func (f *InboundFlow) PublishValidatedInboundAndWait(ctx context.Context, platformMessageID string, msg busruntime.BusMessage) (bool, error) {
	return f.publishValidatedInbound(ctx, platformMessageID, msg, true)
}

func (f *InboundFlow) publishValidatedInbound(ctx context.Context, platformMessageID string, msg busruntime.BusMessage, wait bool) (bool, error) {
	if f == nil {
		return false, fmt.Errorf("inbound flow is not initialized")
	}
	if ctx == nil {
		return false, fmt.Errorf("context is required")
	}
	platformMessageID = strings.TrimSpace(platformMessageID)
	if platformMessageID == "" {
		return false, fmt.Errorf("platform_message_id is required")
	}
	if msg.Channel != "" && strings.ToLower(strings.TrimSpace(string(msg.Channel))) != f.channel {
		return false, fmt.Errorf("message channel mismatch: flow=%s message=%s", f.channel, msg.Channel)
	}

	_, found, err := f.store.GetBusInboxRecord(ctx, f.channel, platformMessageID)
	if err != nil {
		return false, err
	}
	if found {
		return false, nil
	}

	var publishErr error
	if wait {
		publishErr = f.bus.PublishValidatedAndWait(ctx, msg)
	} else {
		publishErr = f.bus.PublishValidated(ctx, msg)
	}
	if publishErr != nil {
		return false, publishErr
	}

	seenAt := f.nowFn().UTC()
	record := contacts.BusInboxRecord{
		Channel:           f.channel,
		PlatformMessageID: platformMessageID,
		ConversationKey:   msg.ConversationKey,
		SeenAt:            seenAt,
	}
	if err := f.store.PutBusInboxRecord(ctx, record); err != nil {
		return false, err
	}
	return true, nil
}

func NormalizeImageAttachments(items []busruntime.ImageAttachment) ([]busruntime.ImageAttachment, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]busruntime.ImageAttachment, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		item := busruntime.ImageAttachment{
			Path:               strings.TrimSpace(raw.Path),
			SourceMessageID:    strings.TrimSpace(raw.SourceMessageID),
			SourceAttachmentID: strings.TrimSpace(raw.SourceAttachmentID),
			MIMEType:           strings.TrimSpace(raw.MIMEType),
		}
		if item.Path == "" {
			return nil, fmt.Errorf("image attachment path is required")
		}
		key := item.Path + "\x00" + item.SourceMessageID + "\x00" + item.SourceAttachmentID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out, nil
}

func NormalizeImageInputs(attachments []busruntime.ImageAttachment, fallbackPaths []string) ([]busruntime.ImageAttachment, []string, error) {
	normalized, err := NormalizeImageAttachments(attachments)
	if err != nil {
		return nil, nil, err
	}
	if len(normalized) == 0 {
		paths, err := NormalizeImagePaths(fallbackPaths)
		if err != nil {
			return nil, nil, err
		}
		normalized = make([]busruntime.ImageAttachment, 0, len(paths))
		for _, path := range paths {
			normalized = append(normalized, busruntime.ImageAttachment{Path: path})
		}
	}
	return normalized, busruntime.ImagePathsFromAttachments(normalized), nil
}

func NormalizeImagePaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			return nil, fmt.Errorf("image path is required")
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out, nil
}
