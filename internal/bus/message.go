package bus

import (
	"fmt"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channels"
)

type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

type Channel string

const (
	ChannelConsole  Channel = Channel(channels.Console)
	ChannelTelegram Channel = Channel(channels.Telegram)
	ChannelSlack    Channel = Channel(channels.Slack)
	ChannelLine     Channel = Channel(channels.Line)
	ChannelLark     Channel = Channel(channels.Lark)
	ChannelDiscord  Channel = Channel(channels.Discord)
)

type MessageExtensions struct {
	PlatformMessageID string   `json:"platform_message_id,omitempty"`
	ReplyTo           string   `json:"reply_to,omitempty"`
	SessionID         string   `json:"session_id,omitempty"`
	ChatType          string   `json:"chat_type,omitempty"`
	FromUserID        int64    `json:"from_user_id,omitempty"`
	FromUsername      string   `json:"from_username,omitempty"`
	FromFirstName     string   `json:"from_first_name,omitempty"`
	FromLastName      string   `json:"from_last_name,omitempty"`
	FromDisplayName   string   `json:"from_display_name,omitempty"`
	TeamID            string   `json:"team_id,omitempty"`
	ChannelID         string   `json:"channel_id,omitempty"`
	FromUserRef       string   `json:"from_user_ref,omitempty"`
	ThreadTS          string   `json:"thread_ts,omitempty"`
	EventID           string   `json:"event_id,omitempty"`
	MentionUsers      []string `json:"mention_users,omitempty"`
	ImagePaths        []string `json:"image_paths,omitempty"`
	ImagePending      bool     `json:"image_pending,omitempty"`
}

type BusMessage struct {
	ID              string            `json:"id"`
	Direction       Direction         `json:"direction"`
	Channel         Channel           `json:"channel"`
	Topic           string            `json:"topic"`
	ConversationKey string            `json:"conversation_key"`
	ParticipantKey  string            `json:"participant_key"`
	IdempotencyKey  string            `json:"idempotency_key"`
	CorrelationID   string            `json:"correlation_id"`
	CausationID     string            `json:"causation_id,omitempty"`
	PayloadBase64   string            `json:"payload_base64"`
	CreatedAt       time.Time         `json:"created_at"`
	Extensions      MessageExtensions `json:"extensions,omitempty"`
}

func (m BusMessage) Validate() error {
	if m.ID != "" {
		if err := validateOptionalCanonicalString("id", m.ID); err != nil {
			return err
		}
	}
	if m.Direction != "" {
		switch m.Direction {
		case DirectionInbound, DirectionOutbound:
		default:
			return fmt.Errorf("direction must be inbound|outbound")
		}
	}

	if m.Channel != "" {
		switch m.Channel {
		case ChannelConsole, ChannelTelegram, ChannelSlack, ChannelLine, ChannelLark, ChannelDiscord:
		default:
			return fmt.Errorf("channel is invalid")
		}
	}

	if err := ValidateTopic(m.Topic); err != nil {
		return wrapError(CodeInvalidTopic, err)
	}
	if err := validateRequiredCanonicalString("conversation_key", m.ConversationKey); err != nil {
		return err
	}
	if m.ParticipantKey != "" {
		if err := validateOptionalCanonicalString("participant_key", m.ParticipantKey); err != nil {
			return err
		}
	}
	if err := validateRequiredCanonicalString("idempotency_key", m.IdempotencyKey); err != nil {
		return err
	}
	if m.CorrelationID != "" {
		if err := validateOptionalCanonicalString("correlation_id", m.CorrelationID); err != nil {
			return err
		}
	}
	if m.CausationID != "" {
		if err := validateOptionalCanonicalString("causation_id", m.CausationID); err != nil {
			return err
		}
	}

	if err := validateRequiredCanonicalString("payload_base64", m.PayloadBase64); err != nil {
		return err
	}
	if _, err := DecodeMessageEnvelope(m.Topic, m.PayloadBase64); err != nil {
		return err
	}
	if m.Extensions.PlatformMessageID != "" {
		if err := validateOptionalCanonicalString("extensions.platform_message_id", m.Extensions.PlatformMessageID); err != nil {
			return err
		}
	}
	if m.Extensions.ReplyTo != "" {
		if err := validateOptionalCanonicalString("extensions.reply_to", m.Extensions.ReplyTo); err != nil {
			return err
		}
	}
	if m.Extensions.SessionID != "" {
		if err := validateUUIDv7Field("extensions.session_id", m.Extensions.SessionID); err != nil {
			return err
		}
	}
	if m.Extensions.ChatType != "" {
		if err := validateOptionalCanonicalString("extensions.chat_type", m.Extensions.ChatType); err != nil {
			return err
		}
	}
	if m.Extensions.FromUsername != "" {
		if err := validateOptionalCanonicalString("extensions.from_username", m.Extensions.FromUsername); err != nil {
			return err
		}
	}
	if m.Extensions.FromFirstName != "" {
		if err := validateOptionalCanonicalString("extensions.from_first_name", m.Extensions.FromFirstName); err != nil {
			return err
		}
	}
	if m.Extensions.FromLastName != "" {
		if err := validateOptionalCanonicalString("extensions.from_last_name", m.Extensions.FromLastName); err != nil {
			return err
		}
	}
	if m.Extensions.FromDisplayName != "" {
		if err := validateOptionalCanonicalString("extensions.from_display_name", m.Extensions.FromDisplayName); err != nil {
			return err
		}
	}
	if m.Extensions.TeamID != "" {
		if err := validateOptionalCanonicalString("extensions.team_id", m.Extensions.TeamID); err != nil {
			return err
		}
	}
	if m.Extensions.ChannelID != "" {
		if err := validateOptionalCanonicalString("extensions.channel_id", m.Extensions.ChannelID); err != nil {
			return err
		}
	}
	if m.Extensions.FromUserRef != "" {
		if err := validateOptionalCanonicalString("extensions.from_user_ref", m.Extensions.FromUserRef); err != nil {
			return err
		}
	}
	if m.Extensions.ThreadTS != "" {
		if err := validateOptionalCanonicalString("extensions.thread_ts", m.Extensions.ThreadTS); err != nil {
			return err
		}
	}
	if m.Extensions.EventID != "" {
		if err := validateOptionalCanonicalString("extensions.event_id", m.Extensions.EventID); err != nil {
			return err
		}
	}
	for i, mention := range m.Extensions.MentionUsers {
		if err := validateRequiredCanonicalString(fmt.Sprintf("extensions.mention_users[%d]", i), mention); err != nil {
			return err
		}
	}
	for i, path := range m.Extensions.ImagePaths {
		if err := validateRequiredCanonicalString(fmt.Sprintf("extensions.image_paths[%d]", i), path); err != nil {
			return err
		}
	}

	return nil
}

func (m BusMessage) Envelope() (MessageEnvelope, error) {
	return DecodeMessageEnvelope(m.Topic, m.PayloadBase64)
}
