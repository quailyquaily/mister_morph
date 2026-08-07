package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/internal/channels"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

const untriggeredTextMaxBytes = 2048

type UntriggeredMessage struct {
	Channel         string
	ConversationKey string
	MessageID       string
	SenderID        string
	SentAt          time.Time
	Text            string
	HasAttachment   bool
}

type UntriggeredRecorder struct {
	journal *domainjournal.Journal
	now     func() time.Time
}

type untriggeredPayload struct {
	Channel         string `json:"channel"`
	ConversationKey string `json:"conversation_key"`
	MessageID       string `json:"message_id"`
	SenderID        string `json:"sender_id,omitempty"`
	SentAt          string `json:"sent_at"`
	Text            string `json:"text,omitempty"`
	TextTruncated   bool   `json:"text_truncated,omitempty"`
	HasAttachment   bool   `json:"has_attachment,omitempty"`
}

func NewUntriggeredRecorder(journalDir string, rotateMaxBytes int64) (*UntriggeredRecorder, error) {
	journal, err := domainjournal.New(domainjournal.JournalOptions{
		Dir:            journalDir,
		RotateMaxBytes: rotateMaxBytes,
		SyncEachWrite:  true,
	})
	if err != nil {
		return nil, err
	}
	return &UntriggeredRecorder{journal: journal, now: time.Now}, nil
}

func (r *UntriggeredRecorder) Record(message UntriggeredMessage) error {
	if r == nil || r.journal == nil {
		return fmt.Errorf("untriggered recorder is required")
	}
	channel := strings.TrimSpace(message.Channel)
	switch channel {
	case channels.Telegram, channels.Slack, channels.Line, channels.Lark:
	default:
		return fmt.Errorf("unsupported channel %q", channel)
	}
	conversationKey := strings.TrimSpace(message.ConversationKey)
	if conversationKey == "" {
		return fmt.Errorf("conversation key is required")
	}
	messageID := strings.TrimSpace(message.MessageID)
	if messageID == "" {
		return fmt.Errorf("message id is required")
	}
	if message.SentAt.IsZero() {
		return fmt.Errorf("sent at is required")
	}

	text, truncated := compactUntriggeredText(message.Text)
	if text == "" && !message.HasAttachment {
		return nil
	}
	payload, err := json.Marshal(untriggeredPayload{
		Channel:         channel,
		ConversationKey: conversationKey,
		MessageID:       messageID,
		SenderID:        strings.TrimSpace(message.SenderID),
		SentAt:          message.SentAt.UTC().Format(time.RFC3339Nano),
		Text:            text,
		TextTruncated:   truncated,
		HasAttachment:   message.HasAttachment,
	})
	if err != nil {
		return fmt.Errorf("encode untriggered message: %w", err)
	}
	now := time.Now
	if r.now != nil {
		now = r.now
	}
	_, err = r.journal.Append(domainjournal.Event{
		ID:            "evt_" + uuid.NewString(),
		Time:          now().UTC().Format(time.RFC3339Nano),
		Domain:        "conversation",
		Type:          "untriggered_inbound",
		SchemaVersion: 1,
		Trace:         domainjournal.Trace{Runtime: channel},
		Payload:       payload,
	})
	return err
}

func (r *UntriggeredRecorder) Close() error {
	if r == nil || r.journal == nil {
		return nil
	}
	return r.journal.Close()
}

func compactUntriggeredText(text string) (string, bool) {
	text = strings.ToValidUTF8(text, "\uFFFD")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSpace(text)
	if len(text) <= untriggeredTextMaxBytes {
		return text, false
	}
	cut := untriggeredTextMaxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}
