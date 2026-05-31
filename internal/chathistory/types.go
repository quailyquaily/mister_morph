package chathistory

import "time"

const (
	ChannelTelegram = "telegram"
	ChannelSlack    = "slack"
	ChannelLine     = "line"
	ChannelLark     = "lark"

	KindInboundUser      = "inbound_user"
	KindInboundReaction  = "inbound_reaction"
	KindOutboundAgent    = "outbound_agent"
	KindOutboundReaction = "outbound_reaction"
	KindSystem           = "system"
)

type ChatHistoryItem struct {
	Channel          string             `json:"channel"`
	Kind             string             `json:"kind"`
	ChatID           string             `json:"chat_id,omitempty"`
	ChatType         string             `json:"chat_type,omitempty"`
	MessageID        string             `json:"message_id,omitempty"`
	ReplyToMessageID string             `json:"reply_to_message_id,omitempty"`
	SentAt           time.Time          `json:"sent_at"`
	Sender           ChatHistorySender  `json:"sender"`
	Text             string             `json:"text"`
	Quote            *ChatHistoryQuote  `json:"quote,omitempty"`
	Images           []ChatHistoryImage `json:"images,omitempty"`
}

type ChatHistorySender struct {
	UserID     string `json:"user_id,omitempty"`
	Username   string `json:"username,omitempty"`
	Nickname   string `json:"nickname,omitempty"`
	IsBot      bool   `json:"is_bot,omitempty"`
	DisplayRef string `json:"display_ref,omitempty"`
}

type ChatHistoryQuote struct {
	SenderRef     string `json:"sender_ref,omitempty"`
	Text          string `json:"text,omitempty"`
	MarkdownBlock string `json:"markdown_block,omitempty"`
}

type ChatHistoryImage struct {
	ID                 string `json:"image_id"`
	Path               string `json:"path,omitempty"`
	MIMEType           string `json:"mime_type,omitempty"`
	Width              int    `json:"width,omitempty"`
	Height             int    `json:"height,omitempty"`
	Bytes              int64  `json:"bytes,omitempty"`
	ContentSHA256      string `json:"content_sha256,omitempty"`
	SourceMessageID    string `json:"source_message_id,omitempty"`
	SourceAttachmentID string `json:"source_attachment_id,omitempty"`
	Description        string `json:"description,omitempty"`
	DescriptionSource  string `json:"description_source,omitempty"`
}
