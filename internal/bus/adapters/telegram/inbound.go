package telegram

import (
	"context"
	"fmt"
	"strconv"
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
	ChatID              int64
	MessageThreadID     int64
	MessageID           int64
	ReplyToMessageID    int64
	SentAt              time.Time
	ChatType            string
	FromUserID          int64
	FromUsername        string
	FromFirstName       string
	FromLastName        string
	FromDisplayName     string
	FromIsAgent         bool
	Text                string
	MentionUsers        []string
	MentionParticipants []busruntime.MessageParticipant
	ImagePaths          []string
	ImageAttachments    []busruntime.ImageAttachment
}

type InboundAdapter struct {
	flow  *baseadapters.InboundFlow
	nowFn func() time.Time
}

func NewInboundAdapter(opts InboundAdapterOptions) (*InboundAdapter, error) {
	flow, err := baseadapters.NewInboundFlow(baseadapters.InboundFlowOptions{
		Bus:     opts.Bus,
		Store:   opts.Store,
		Channel: string(busruntime.ChannelTelegram),
		Now:     opts.Now,
	})
	if err != nil {
		return nil, err
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &InboundAdapter{
		flow:  flow,
		nowFn: nowFn,
	}, nil
}

func (a *InboundAdapter) HandleInboundMessage(ctx context.Context, msg InboundMessage) (bool, error) {
	if a == nil || a.flow == nil {
		return false, fmt.Errorf("telegram inbound adapter is not initialized")
	}
	if ctx == nil {
		return false, fmt.Errorf("context is required")
	}
	chatID := msg.ChatID
	if chatID == 0 {
		return false, fmt.Errorf("chat_id is required")
	}
	messageThreadID := msg.MessageThreadID
	if messageThreadID < 0 {
		return false, fmt.Errorf("message_thread_id is invalid")
	}
	messageID := msg.MessageID
	if messageID == 0 {
		return false, fmt.Errorf("message_id is required")
	}
	replyToMessageID := msg.ReplyToMessageID
	if replyToMessageID < 0 {
		return false, fmt.Errorf("reply_to_message_id is invalid")
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return false, fmt.Errorf("text is required")
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
	envelopeMessageID := fmt.Sprintf("telegram:%d:%d", chatID, messageID)
	replyTo := ""
	if replyToMessageID > 0 {
		replyTo = strconv.FormatInt(replyToMessageID, 10)
	}
	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: envelopeMessageID,
		Text:      text,
		SentAt:    sentAt.Format(time.RFC3339),
		SessionID: sessionID,
		ReplyTo:   replyTo,
	})
	if err != nil {
		return false, err
	}
	conversationKey, err := busruntime.BuildTelegramTopicConversationKey(strconv.FormatInt(chatID, 10), messageThreadID)
	if err != nil {
		return false, err
	}

	participantKey := ""
	if msg.FromUserID != 0 {
		participantKey = strconv.FormatInt(msg.FromUserID, 10)
	}
	chatType := strings.TrimSpace(msg.ChatType)
	if chatType == "" {
		return false, fmt.Errorf("chat_type is required")
	}
	mentionUsers, err := normalizeMentionUsers(msg.MentionUsers)
	if err != nil {
		return false, err
	}
	mentionParticipants, err := normalizeMentionParticipants(msg.MentionParticipants)
	if err != nil {
		return false, err
	}
	imageAttachments, imagePaths, err := baseadapters.NormalizeImageInputs(msg.ImageAttachments, msg.ImagePaths)
	if err != nil {
		return false, err
	}

	busMsg := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelTelegram,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		ParticipantKey:  participantKey,
		IdempotencyKey:  idempotency.MessageEnvelopeKey(envelopeMessageID),
		CorrelationID:   fmt.Sprintf("telegram:%d:%d", chatID, messageID),
		PayloadBase64:   payloadBase64,
		CreatedAt:       sentAt,
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID:   fmt.Sprintf("%d:%d", chatID, messageID),
			ReplyTo:             replyTo,
			SessionID:           sessionID,
			ChatType:            chatType,
			MessageThreadID:     messageThreadID,
			FromUserID:          msg.FromUserID,
			FromUsername:        strings.TrimSpace(msg.FromUsername),
			FromFirstName:       strings.TrimSpace(msg.FromFirstName),
			FromLastName:        strings.TrimSpace(msg.FromLastName),
			FromDisplayName:     strings.TrimSpace(msg.FromDisplayName),
			FromIsAgent:         msg.FromIsAgent,
			MentionUsers:        mentionUsers,
			MentionParticipants: mentionParticipants,
			ImagePaths:          imagePaths,
			ImageAttachments:    imageAttachments,
		},
	}
	platformMessageID := fmt.Sprintf("%d:%d", chatID, messageID)
	return a.flow.PublishValidatedInbound(ctx, platformMessageID, busMsg)
}

func InboundMessageFromBusMessage(msg busruntime.BusMessage) (InboundMessage, error) {
	if msg.Direction != busruntime.DirectionInbound {
		return InboundMessage{}, fmt.Errorf("direction must be inbound")
	}
	if msg.Channel != busruntime.ChannelTelegram {
		return InboundMessage{}, fmt.Errorf("channel must be telegram")
	}
	chatID, messageThreadID, err := ConversationPartsFromBusMessage(msg)
	if err != nil {
		return InboundMessage{}, err
	}
	messageID, err := parseTelegramMessageID(msg.Extensions.PlatformMessageID)
	if err != nil {
		return InboundMessage{}, err
	}
	envelope, err := msg.Envelope()
	if err != nil {
		return InboundMessage{}, err
	}
	replyToRaw := strings.TrimSpace(msg.Extensions.ReplyTo)
	if replyToRaw == "" {
		replyToRaw = strings.TrimSpace(envelope.ReplyTo)
	}
	replyToMessageID, err := parseOptionalTelegramReplyToMessageID(replyToRaw)
	if err != nil {
		return InboundMessage{}, err
	}
	sentAt, err := time.Parse(time.RFC3339, strings.TrimSpace(envelope.SentAt))
	if err != nil {
		return InboundMessage{}, fmt.Errorf("sent_at is invalid")
	}
	if strings.TrimSpace(msg.Extensions.ChatType) == "" {
		return InboundMessage{}, fmt.Errorf("chat_type is required")
	}
	mentionUsers, err := normalizeMentionUsers(msg.Extensions.MentionUsers)
	if err != nil {
		return InboundMessage{}, err
	}
	mentionParticipants, err := normalizeMentionParticipants(msg.Extensions.MentionParticipants)
	if err != nil {
		return InboundMessage{}, err
	}
	imageAttachments, imagePaths, err := baseadapters.NormalizeImageInputs(msg.Extensions.ImageAttachments, msg.Extensions.ImagePaths)
	if err != nil {
		return InboundMessage{}, err
	}

	return InboundMessage{
		ChatID:              chatID,
		MessageThreadID:     messageThreadID,
		MessageID:           messageID,
		ReplyToMessageID:    replyToMessageID,
		SentAt:              sentAt.UTC(),
		ChatType:            strings.TrimSpace(msg.Extensions.ChatType),
		FromUserID:          msg.Extensions.FromUserID,
		FromUsername:        strings.TrimSpace(msg.Extensions.FromUsername),
		FromFirstName:       strings.TrimSpace(msg.Extensions.FromFirstName),
		FromLastName:        strings.TrimSpace(msg.Extensions.FromLastName),
		FromDisplayName:     strings.TrimSpace(msg.Extensions.FromDisplayName),
		FromIsAgent:         msg.Extensions.FromIsAgent,
		Text:                strings.TrimSpace(envelope.Text),
		MentionUsers:        mentionUsers,
		MentionParticipants: mentionParticipants,
		ImagePaths:          imagePaths,
		ImageAttachments:    imageAttachments,
	}, nil
}

func parseTelegramMessageID(platformMessageID string) (int64, error) {
	platformMessageID = strings.TrimSpace(platformMessageID)
	if platformMessageID == "" {
		return 0, fmt.Errorf("platform_message_id is required")
	}
	parts := strings.Split(platformMessageID, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("platform_message_id is invalid")
	}
	messageID, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("platform_message_id is invalid: %w", err)
	}
	if messageID == 0 {
		return 0, fmt.Errorf("platform_message_id is invalid")
	}
	return messageID, nil
}

func parseOptionalTelegramReplyToMessageID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	replyToMessageID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || replyToMessageID <= 0 {
		return 0, fmt.Errorf("reply_to is invalid")
	}
	return replyToMessageID, nil
}

func normalizeMentionUsers(items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" {
			return nil, fmt.Errorf("mention user is required")
		}
		out = append(out, item)
	}
	return out, nil
}

func normalizeMentionParticipants(items []busruntime.MessageParticipant) ([]busruntime.MessageParticipant, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]busruntime.MessageParticipant, 0, len(items))
	for _, participant := range items {
		participant.ID = strings.TrimSpace(participant.ID)
		participant.Nickname = strings.TrimSpace(participant.Nickname)
		if participant.ID == "" {
			return nil, fmt.Errorf("mention participant id is required")
		}
		out = append(out, participant)
	}
	return out, nil
}
