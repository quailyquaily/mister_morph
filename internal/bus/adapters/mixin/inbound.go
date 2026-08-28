package mixin

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

type InboundMessage struct {
	ConversationID   string
	MessageID        string
	SentAt           time.Time
	ChatType         string
	FromUserID       string
	IdentityNumber   string
	DisplayName      string
	FromIsAgent      bool
	Text             string
	QuoteMessageID   string
	MentionUserIDs   []string
	ImageAttachments []busruntime.ImageAttachment
	ConversationName string
}

type InboundAdapter struct {
	flow  *baseadapters.InboundFlow
	nowFn func() time.Time
}

func NewInboundAdapter(opts InboundAdapterOptions) (*InboundAdapter, error) {
	flow, err := baseadapters.NewInboundFlow(baseadapters.InboundFlowOptions{
		Bus:     opts.Bus,
		Store:   opts.Store,
		Channel: string(busruntime.ChannelMixin),
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
		return false, fmt.Errorf("mixin inbound adapter is not initialized")
	}
	if ctx == nil {
		return false, fmt.Errorf("context is required")
	}
	conversationID, err := normalizeUUID("conversation_id", msg.ConversationID)
	if err != nil {
		return false, err
	}
	messageID, err := normalizeUUID("message_id", msg.MessageID)
	if err != nil {
		return false, err
	}
	fromUserID, err := normalizeUUID("from_user_id", msg.FromUserID)
	if err != nil {
		return false, err
	}
	chatType, err := normalizeChatType(msg.ChatType)
	if err != nil {
		return false, err
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return false, fmt.Errorf("text is required")
	}
	quoteMessageID := ""
	if strings.TrimSpace(msg.QuoteMessageID) != "" {
		quoteMessageID, err = normalizeUUID("quote_message_id", msg.QuoteMessageID)
		if err != nil {
			return false, err
		}
	}
	mentionUserIDs, err := normalizeUserIDs(msg.MentionUserIDs)
	if err != nil {
		return false, err
	}
	imageAttachments, imagePaths, err := baseadapters.NormalizeImageInputs(msg.ImageAttachments, nil)
	if err != nil {
		return false, err
	}
	sentAt := msg.SentAt.UTC()
	if sentAt.IsZero() {
		sentAt = a.nowFn().UTC()
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return false, err
	}
	platformMessageID := conversationID + ":" + messageID
	envelopeMessageID := "mixin:" + platformMessageID
	payload, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: envelopeMessageID,
		Text:      text,
		SentAt:    sentAt.Format(time.RFC3339),
		SessionID: sessionID.String(),
		ReplyTo:   quoteMessageID,
	})
	if err != nil {
		return false, err
	}
	conversationKey, err := busruntime.BuildMixinConversationKey(conversationID)
	if err != nil {
		return false, err
	}
	message := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelMixin,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		ParticipantKey:  fromUserID,
		IdempotencyKey:  idempotency.MessageEnvelopeKey(envelopeMessageID),
		CorrelationID:   "mixin:" + platformMessageID,
		PayloadBase64:   payload,
		CreatedAt:       sentAt,
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: platformMessageID,
			ReplyTo:           quoteMessageID,
			SessionID:         sessionID.String(),
			ChatType:          chatType,
			FromUsername:      strings.TrimSpace(msg.IdentityNumber),
			FromDisplayName:   strings.TrimSpace(msg.DisplayName),
			FromIsAgent:       msg.FromIsAgent,
			ChannelID:         conversationID,
			FromUserRef:       fromUserID,
			EventID:           messageID,
			MentionUsers:      mentionUserIDs,
			ImagePaths:        imagePaths,
			ImageAttachments:  imageAttachments,
		},
	}
	return a.flow.PublishValidatedInbound(ctx, platformMessageID, message)
}

func InboundMessageFromBusMessage(msg busruntime.BusMessage) (InboundMessage, error) {
	if msg.Direction != busruntime.DirectionInbound {
		return InboundMessage{}, fmt.Errorf("direction must be inbound")
	}
	if msg.Channel != busruntime.ChannelMixin {
		return InboundMessage{}, fmt.Errorf("channel must be mixin")
	}
	conversationID, err := busruntime.ParseMixinConversationKey(msg.ConversationKey)
	if err != nil {
		return InboundMessage{}, err
	}
	platformConversationID, messageID, err := parsePlatformMessageID(msg.Extensions.PlatformMessageID)
	if err != nil {
		return InboundMessage{}, err
	}
	if platformConversationID != conversationID {
		return InboundMessage{}, fmt.Errorf("platform_message_id does not match conversation_key")
	}
	envelope, err := msg.Envelope()
	if err != nil {
		return InboundMessage{}, err
	}
	sentAt, err := time.Parse(time.RFC3339, strings.TrimSpace(envelope.SentAt))
	if err != nil {
		return InboundMessage{}, fmt.Errorf("sent_at is invalid")
	}
	chatType, err := normalizeChatType(msg.Extensions.ChatType)
	if err != nil {
		return InboundMessage{}, err
	}
	fromUserID, err := normalizeUUID("from_user_id", firstNonEmpty(msg.Extensions.FromUserRef, msg.ParticipantKey))
	if err != nil {
		return InboundMessage{}, err
	}
	mentionUserIDs, err := normalizeUserIDs(msg.Extensions.MentionUsers)
	if err != nil {
		return InboundMessage{}, err
	}
	imageAttachments, _, err := baseadapters.NormalizeImageInputs(msg.Extensions.ImageAttachments, msg.Extensions.ImagePaths)
	if err != nil {
		return InboundMessage{}, err
	}
	return InboundMessage{
		ConversationID:   conversationID,
		MessageID:        messageID,
		SentAt:           sentAt.UTC(),
		ChatType:         chatType,
		FromUserID:       fromUserID,
		IdentityNumber:   strings.TrimSpace(msg.Extensions.FromUsername),
		DisplayName:      strings.TrimSpace(msg.Extensions.FromDisplayName),
		FromIsAgent:      msg.Extensions.FromIsAgent,
		Text:             strings.TrimSpace(envelope.Text),
		QuoteMessageID:   strings.TrimSpace(msg.Extensions.ReplyTo),
		MentionUserIDs:   mentionUserIDs,
		ImageAttachments: imageAttachments,
	}, nil
}

func parsePlatformMessageID(value string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("platform_message_id is invalid")
	}
	conversationID, err := normalizeUUID("conversation_id", parts[0])
	if err != nil {
		return "", "", err
	}
	messageID, err := normalizeUUID("message_id", parts[1])
	if err != nil {
		return "", "", err
	}
	return conversationID, messageID, nil
}

func normalizeUUID(field, value string) (string, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil || id == uuid.Nil {
		return "", fmt.Errorf("%s is invalid", field)
	}
	return id.String(), nil
}

func normalizeChatType(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value != "CONTACT" && value != "GROUP" {
		return "", fmt.Errorf("chat_type must be CONTACT or GROUP")
	}
	return value, nil
}

func normalizeUserIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		id, err := normalizeUUID("mention_user_id", value)
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
