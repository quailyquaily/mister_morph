package lark

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	baseadapters "github.com/quailyquaily/mistermorph/internal/bus/adapters"
	"github.com/quailyquaily/mistermorph/internal/idempotency"
)

type InboundAdapterOptions struct {
	Bus   *busruntime.Inproc
	Store baseadapters.InboundStore
	Now   func() time.Time
}

// InboundMessage is the normalized Lark message event for bus ingress.
type InboundMessage struct {
	ChatID       string
	MessageID    string
	SentAt       time.Time
	ChatType     string
	FromUserID   string
	DisplayName  string
	Text         string
	MentionUsers []string
	EventID      string
	ImagePaths   []string
}

type InboundAdapter struct {
	flow  *baseadapters.InboundFlow
	nowFn func() time.Time
}

func NewInboundAdapter(opts InboundAdapterOptions) (*InboundAdapter, error) {
	flow, err := baseadapters.NewInboundFlow(baseadapters.InboundFlowOptions{
		Bus:     opts.Bus,
		Store:   opts.Store,
		Channel: string(busruntime.ChannelLark),
		Now:     opts.Now,
	})
	if err != nil {
		return nil, err
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &InboundAdapter{flow: flow, nowFn: nowFn}, nil
}

func (a *InboundAdapter) HandleInboundMessage(ctx context.Context, msg InboundMessage) (bool, error) {
	if a == nil || a.flow == nil {
		return false, fmt.Errorf("lark inbound adapter is not initialized")
	}
	if ctx == nil {
		return false, fmt.Errorf("context is required")
	}
	chatID := strings.TrimSpace(msg.ChatID)
	if chatID == "" {
		return false, fmt.Errorf("chat_id is required")
	}
	messageID := strings.TrimSpace(msg.MessageID)
	if messageID == "" {
		return false, fmt.Errorf("message_id is required")
	}
	chatType, err := normalizeLarkChatType(msg.ChatType)
	if err != nil {
		return false, err
	}
	fromUserID := strings.TrimSpace(msg.FromUserID)
	if fromUserID == "" {
		return false, fmt.Errorf("from_user_id is required")
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return false, fmt.Errorf("text is required")
	}
	mentionUsers, err := normalizeLarkMentionUsers(msg.MentionUsers)
	if err != nil {
		return false, err
	}
	imagePaths, err := normalizeImagePaths(msg.ImagePaths)
	if err != nil {
		return false, err
	}

	now := a.nowFn().UTC()
	sentAt := msg.SentAt.UTC()
	if sentAt.IsZero() {
		sentAt = now
	}

	sessionUUID, err := uuid.NewV7()
	if err != nil {
		return false, err
	}
	sessionID := sessionUUID.String()
	envelopeMessageID := larkEnvelopeMessageID(chatID, messageID)
	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: envelopeMessageID,
		Text:      text,
		SentAt:    sentAt.Format(time.RFC3339),
		SessionID: sessionID,
		ReplyTo:   messageID,
	})
	if err != nil {
		return false, err
	}

	conversationKey, err := busruntime.BuildLarkConversationKey(chatID)
	if err != nil {
		return false, err
	}
	platformMessageID := larkPlatformMessageID(chatID, messageID)

	busMsg := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelLark,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		ParticipantKey:  fromUserID,
		IdempotencyKey:  idempotency.MessageEnvelopeKey(envelopeMessageID),
		CorrelationID:   "lark:" + platformMessageID,
		PayloadBase64:   payloadBase64,
		CreatedAt:       sentAt,
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: platformMessageID,
			ReplyTo:           messageID,
			SessionID:         sessionID,
			ChatType:          chatType,
			FromDisplayName:   strings.TrimSpace(msg.DisplayName),
			ChannelID:         chatID,
			FromUserRef:       fromUserID,
			EventID:           strings.TrimSpace(msg.EventID),
			MentionUsers:      mentionUsers,
			ImagePaths:        imagePaths,
		},
	}
	return a.flow.PublishValidatedInbound(ctx, platformMessageID, busMsg)
}

func InboundMessageFromBusMessage(msg busruntime.BusMessage) (InboundMessage, error) {
	if msg.Direction != busruntime.DirectionInbound {
		return InboundMessage{}, fmt.Errorf("direction must be inbound")
	}
	if msg.Channel != busruntime.ChannelLark {
		return InboundMessage{}, fmt.Errorf("channel must be lark")
	}
	chatID, err := chatIDFromConversationKey(msg.ConversationKey)
	if err != nil {
		return InboundMessage{}, err
	}
	pmChatID, messageID, err := parseLarkPlatformMessageID(msg.Extensions.PlatformMessageID)
	if err != nil {
		return InboundMessage{}, err
	}
	if pmChatID != chatID {
		return InboundMessage{}, fmt.Errorf("platform_message_id does not match conversation_key")
	}
	env, err := msg.Envelope()
	if err != nil {
		return InboundMessage{}, err
	}
	sentAt, err := time.Parse(time.RFC3339, strings.TrimSpace(env.SentAt))
	if err != nil {
		return InboundMessage{}, fmt.Errorf("sent_at is invalid")
	}
	chatType, err := normalizeLarkChatType(msg.Extensions.ChatType)
	if err != nil {
		return InboundMessage{}, err
	}
	fromUserID := strings.TrimSpace(msg.Extensions.FromUserRef)
	if fromUserID == "" {
		fromUserID = strings.TrimSpace(msg.ParticipantKey)
	}
	if fromUserID == "" {
		return InboundMessage{}, fmt.Errorf("from_user_id is required")
	}
	mentionUsers, err := normalizeLarkMentionUsers(msg.Extensions.MentionUsers)
	if err != nil {
		return InboundMessage{}, err
	}
	imagePaths, err := normalizeImagePaths(msg.Extensions.ImagePaths)
	if err != nil {
		return InboundMessage{}, err
	}

	return InboundMessage{
		ChatID:       chatID,
		MessageID:    messageID,
		SentAt:       sentAt.UTC(),
		ChatType:     chatType,
		FromUserID:   fromUserID,
		DisplayName:  strings.TrimSpace(msg.Extensions.FromDisplayName),
		Text:         strings.TrimSpace(env.Text),
		MentionUsers: mentionUsers,
		EventID:      strings.TrimSpace(msg.Extensions.EventID),
		ImagePaths:   imagePaths,
	}, nil
}

func larkEnvelopeMessageID(chatID, messageID string) string {
	return "lark:" + larkPlatformMessageID(chatID, messageID)
}

func larkPlatformMessageID(chatID, messageID string) string {
	return strings.TrimSpace(chatID) + ":" + strings.TrimSpace(messageID)
}

func parseLarkPlatformMessageID(platformMessageID string) (string, string, error) {
	platformMessageID = strings.TrimSpace(platformMessageID)
	if platformMessageID == "" {
		return "", "", fmt.Errorf("platform_message_id is required")
	}
	parts := strings.Split(platformMessageID, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("platform_message_id is invalid")
	}
	chatID := strings.TrimSpace(parts[0])
	messageID := strings.TrimSpace(parts[1])
	if chatID == "" || messageID == "" {
		return "", "", fmt.Errorf("platform_message_id is invalid")
	}
	return chatID, messageID, nil
}

func normalizeLarkMentionUsers(items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			return nil, fmt.Errorf("mention user is required")
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out, nil
}

func chatIDFromConversationKey(conversationKey string) (string, error) {
	const prefix = "lark:"
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(conversationKey)), prefix) {
		return "", fmt.Errorf("lark conversation key is invalid")
	}
	chatID := strings.TrimSpace(strings.TrimSpace(conversationKey)[len(prefix):])
	if chatID == "" {
		return "", fmt.Errorf("lark chat id is required")
	}
	return chatID, nil
}

func normalizeImagePaths(paths []string) ([]string, error) {
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

func normalizeLarkChatType(raw string) (string, error) {
	chatType := strings.ToLower(strings.TrimSpace(raw))
	switch chatType {
	case "group", "private":
		return chatType, nil
	case "p2p":
		return "private", nil
	case "topic_group":
		return "group", nil
	case "":
		return "", fmt.Errorf("chat_type is required")
	default:
		return "", fmt.Errorf("lark chat_type %q is not supported", chatType)
	}
}
