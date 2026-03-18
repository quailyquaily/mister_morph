package contacts

import (
	"time"

	"github.com/quailyquaily/mistermorph/internal/channels"
)

type Kind string

const (
	KindHuman Kind = "human"
	KindAgent Kind = "agent"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

const (
	ChannelConsole  = channels.Console
	ChannelTelegram = channels.Telegram
	ChannelSlack    = channels.Slack
	ChannelLine     = channels.Line
	ChannelLark     = channels.Lark
	ShareTopic      = "chat.message"
)

type Contact struct {
	ContactID         string     `json:"contact_id"`
	Kind              Kind       `json:"kind"`
	Channel           string     `json:"channel"`
	ContactNickname   string     `json:"nickname,omitempty"`
	TGUsername        string     `json:"tg_username,omitempty"`
	TGPrivateChatID   int64      `json:"tg_private_chat_id,omitempty"`
	TGGroupChatIDs    []int64    `json:"tg_group_chat_ids,omitempty"`
	LineUserID        string     `json:"line_user_id,omitempty"`
	LineChatIDs       []string   `json:"line_chat_ids,omitempty"`
	LarkOpenID        string     `json:"lark_open_id,omitempty"`
	LarkChatIDs       []string   `json:"lark_chat_ids,omitempty"`
	SlackTeamID       string     `json:"slack_team_id,omitempty"`
	SlackUserID       string     `json:"slack_user_id,omitempty"`
	SlackDMChannelID  string     `json:"slack_dm_channel_id,omitempty"`
	SlackChannelIDs   []string   `json:"slack_channel_ids,omitempty"`
	PersonaBrief      string     `json:"persona_brief,omitempty"`
	TopicPreferences  []string   `json:"topic_preferences,omitempty"`
	CooldownUntil     *time.Time `json:"cooldown_until,omitempty"`
	LastInteractionAt *time.Time `json:"last_interaction_at,omitempty"`
}

type ShareDecision struct {
	ContactID      string `json:"contact_id"`
	ChatID         string `json:"chat_id,omitempty"`
	PeerID         string `json:"peer_id,omitempty"`
	ItemID         string `json:"item_id"`
	ContentType    string `json:"content_type"`
	PayloadBase64  string `json:"payload_base64"`
	IdempotencyKey string `json:"idempotency_key"`
}

type ShareOutcome struct {
	ContactID      string    `json:"contact_id"`
	PeerID         string    `json:"peer_id,omitempty"`
	ItemID         string    `json:"item_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Accepted       bool      `json:"accepted"`
	Deduped        bool      `json:"deduped"`
	Error          string    `json:"error,omitempty"`
	SentAt         time.Time `json:"sent_at"`
}
