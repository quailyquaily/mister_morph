package slack

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
	TeamID       string
	ChannelID    string
	ChatType     string
	MessageTS    string
	ThreadTS     string
	UserID       string
	Username     string
	DisplayName  string
	Text         string
	SentAt       time.Time
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
		Channel: string(busruntime.ChannelSlack),
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
		return false, fmt.Errorf("slack inbound adapter is not initialized")
	}
	if ctx == nil {
		return false, fmt.Errorf("context is required")
	}
	teamID := strings.TrimSpace(msg.TeamID)
	if teamID == "" {
		return false, fmt.Errorf("team_id is required")
	}
	channelID := strings.TrimSpace(msg.ChannelID)
	if channelID == "" {
		return false, fmt.Errorf("channel_id is required")
	}
	chatType := strings.TrimSpace(msg.ChatType)
	if chatType == "" {
		return false, fmt.Errorf("chat_type is required")
	}
	messageTS := strings.TrimSpace(msg.MessageTS)
	if messageTS == "" {
		return false, fmt.Errorf("message_ts is required")
	}
	threadTS := strings.TrimSpace(msg.ThreadTS)
	userID := strings.TrimSpace(msg.UserID)
	if userID == "" {
		return false, fmt.Errorf("user_id is required")
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return false, fmt.Errorf("text is required")
	}
	mentionUsers, err := normalizeMentionUsers(msg.MentionUsers)
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
	envelopeMessageID := slackEnvelopeMessageID(teamID, channelID, messageTS)
	payloadBase64, err := busruntime.EncodeMessageEnvelope(busruntime.TopicChatMessage, busruntime.MessageEnvelope{
		MessageID: envelopeMessageID,
		Text:      text,
		SentAt:    sentAt.Format(time.RFC3339),
		SessionID: sessionID,
		ReplyTo:   threadTS,
	})
	if err != nil {
		return false, err
	}
	conversationKey, err := busruntime.BuildSlackChannelConversationKey(slackConversationID(teamID, channelID))
	if err != nil {
		return false, err
	}
	platformMessageID := slackPlatformMessageID(teamID, channelID, messageTS)
	correlationID := "slack:" + platformMessageID
	participantKey := slackParticipantKey(teamID, userID)

	busMsg := busruntime.BusMessage{
		ID:              "bus_" + uuid.NewString(),
		Direction:       busruntime.DirectionInbound,
		Channel:         busruntime.ChannelSlack,
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: conversationKey,
		ParticipantKey:  participantKey,
		IdempotencyKey:  idempotency.MessageEnvelopeKey(envelopeMessageID),
		CorrelationID:   correlationID,
		PayloadBase64:   payloadBase64,
		CreatedAt:       sentAt,
		Extensions: busruntime.MessageExtensions{
			PlatformMessageID: platformMessageID,
			ReplyTo:           threadTS,
			SessionID:         sessionID,
			ChatType:          chatType,
			FromUsername:      strings.TrimSpace(msg.Username),
			FromDisplayName:   strings.TrimSpace(msg.DisplayName),
			TeamID:            teamID,
			ChannelID:         channelID,
			FromUserRef:       userID,
			ThreadTS:          threadTS,
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
	if msg.Channel != busruntime.ChannelSlack {
		return InboundMessage{}, fmt.Errorf("channel must be slack")
	}
	teamID, channelID, err := slackConversationPartsFromKey(msg.ConversationKey)
	if err != nil {
		return InboundMessage{}, err
	}
	pmTeamID, pmChannelID, messageTS, err := parseSlackPlatformMessageID(msg.Extensions.PlatformMessageID)
	if err != nil {
		return InboundMessage{}, err
	}
	if pmTeamID != teamID || pmChannelID != channelID {
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
	chatType := strings.TrimSpace(msg.Extensions.ChatType)
	if chatType == "" {
		return InboundMessage{}, fmt.Errorf("chat_type is required")
	}
	mentionUsers, err := normalizeMentionUsers(msg.Extensions.MentionUsers)
	if err != nil {
		return InboundMessage{}, err
	}
	imagePaths, err := normalizeImagePaths(msg.Extensions.ImagePaths)
	if err != nil {
		return InboundMessage{}, err
	}
	threadTS := strings.TrimSpace(msg.Extensions.ThreadTS)
	if threadTS == "" {
		threadTS = strings.TrimSpace(msg.Extensions.ReplyTo)
	}
	if threadTS == "" {
		threadTS = strings.TrimSpace(env.ReplyTo)
	}
	userID := strings.TrimSpace(msg.Extensions.FromUserRef)
	if userID == "" {
		_, participantUserID, parseErr := parseSlackParticipantKey(msg.ParticipantKey)
		if parseErr == nil {
			userID = participantUserID
		}
	}

	return InboundMessage{
		TeamID:       teamID,
		ChannelID:    channelID,
		ChatType:     chatType,
		MessageTS:    messageTS,
		ThreadTS:     threadTS,
		UserID:       userID,
		Username:     strings.TrimSpace(msg.Extensions.FromUsername),
		DisplayName:  strings.TrimSpace(msg.Extensions.FromDisplayName),
		Text:         strings.TrimSpace(env.Text),
		SentAt:       sentAt.UTC(),
		MentionUsers: mentionUsers,
		EventID:      strings.TrimSpace(msg.Extensions.EventID),
		ImagePaths:   imagePaths,
	}, nil
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

func slackConversationID(teamID, channelID string) string {
	return strings.TrimSpace(teamID) + ":" + strings.TrimSpace(channelID)
}

func slackEnvelopeMessageID(teamID, channelID, messageTS string) string {
	return "slack:" + slackPlatformMessageID(teamID, channelID, messageTS)
}

func slackPlatformMessageID(teamID, channelID, messageTS string) string {
	return strings.TrimSpace(teamID) + ":" + strings.TrimSpace(channelID) + ":" + strings.TrimSpace(messageTS)
}

func slackParticipantKey(teamID, userID string) string {
	return strings.TrimSpace(teamID) + ":" + strings.TrimSpace(userID)
}

func parseSlackParticipantKey(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("slack participant key is invalid")
	}
	teamID := strings.TrimSpace(parts[0])
	userID := strings.TrimSpace(parts[1])
	if teamID == "" || userID == "" {
		return "", "", fmt.Errorf("slack participant key is invalid")
	}
	return teamID, userID, nil
}

func parseSlackPlatformMessageID(platformMessageID string) (string, string, string, error) {
	platformMessageID = strings.TrimSpace(platformMessageID)
	if platformMessageID == "" {
		return "", "", "", fmt.Errorf("platform_message_id is required")
	}
	parts := strings.Split(platformMessageID, ":")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("platform_message_id is invalid")
	}
	teamID := strings.TrimSpace(parts[0])
	channelID := strings.TrimSpace(parts[1])
	messageTS := strings.TrimSpace(parts[2])
	if teamID == "" || channelID == "" || messageTS == "" {
		return "", "", "", fmt.Errorf("platform_message_id is invalid")
	}
	return teamID, channelID, messageTS, nil
}

func slackConversationPartsFromKey(conversationKey string) (string, string, error) {
	const prefix = "slack:"
	if !strings.HasPrefix(conversationKey, prefix) {
		return "", "", fmt.Errorf("slack conversation key is invalid")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(conversationKey, prefix))
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("slack conversation key is invalid")
	}
	teamID := strings.TrimSpace(parts[0])
	channelID := strings.TrimSpace(parts[1])
	if teamID == "" || channelID == "" {
		return "", "", fmt.Errorf("slack conversation key is invalid")
	}
	return teamID, channelID, nil
}
