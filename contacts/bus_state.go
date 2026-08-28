package contacts

import (
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channels"
)

type BusDeliveryStatus string

const (
	BusDeliveryStatusPending BusDeliveryStatus = "pending"
	BusDeliveryStatusSent    BusDeliveryStatus = "sent"
	BusDeliveryStatusFailed  BusDeliveryStatus = "failed"
)

type BusInboxRecord struct {
	Channel           string    `json:"channel"`
	PlatformMessageID string    `json:"platform_message_id"`
	ConversationKey   string    `json:"conversation_key,omitempty"`
	SeenAt            time.Time `json:"seen_at"`
}

type BusOutboxRecord struct {
	Channel        string            `json:"channel"`
	IdempotencyKey string            `json:"idempotency_key"`
	ContactID      string            `json:"contact_id,omitempty"`
	PeerID         string            `json:"peer_id,omitempty"`
	ItemID         string            `json:"item_id,omitempty"`
	ContentType    string            `json:"content_type,omitempty"`
	PayloadBase64  string            `json:"payload_base64,omitempty"`
	Status         BusDeliveryStatus `json:"status"`
	Attempts       int               `json:"attempts"`
	Accepted       bool              `json:"accepted,omitempty"`
	Deduped        bool              `json:"deduped,omitempty"`
	LastError      string            `json:"last_error,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	LastAttemptAt  *time.Time        `json:"last_attempt_at,omitempty"`
	SentAt         *time.Time        `json:"sent_at,omitempty"`
}

func normalizeBusChannel(channel string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(channel))
	switch value {
	case channels.Telegram, channels.Slack, channels.Line, channels.Lark, channels.Discord, channels.Mixin:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported bus channel: %q", channel)
	}
}

func normalizeBusDeliveryStatus(status BusDeliveryStatus) (BusDeliveryStatus, error) {
	switch status {
	case BusDeliveryStatusPending, BusDeliveryStatusSent, BusDeliveryStatusFailed:
		return status, nil
	default:
		return "", fmt.Errorf("unsupported bus delivery status: %q", status)
	}
}
