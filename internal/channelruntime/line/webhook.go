package line

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	linebus "github.com/quailyquaily/mistermorph/internal/bus/adapters/line"
)

const lineWebhookBodyMaxBytes = 1 << 20 // 1MB

type lineWebhookHandlerOptions struct {
	ChannelSecret string
	Inbound       *linebus.InboundAdapter
	AllowedGroups map[string]bool
	Logger        *slog.Logger
}

type lineWebhookPayload struct {
	Destination string             `json:"destination,omitempty"`
	Events      []lineWebhookEvent `json:"events,omitempty"`
}

type lineWebhookEvent struct {
	Type           string             `json:"type,omitempty"`
	Mode           string             `json:"mode,omitempty"`
	Timestamp      int64              `json:"timestamp,omitempty"`
	ReplyToken     string             `json:"replyToken,omitempty"`
	WebhookEventID string             `json:"webhookEventId,omitempty"`
	Source         lineWebhookSource  `json:"source,omitempty"`
	Message        lineWebhookMessage `json:"message,omitempty"`
}

type lineWebhookSource struct {
	Type    string `json:"type,omitempty"`
	UserID  string `json:"userId,omitempty"`
	GroupID string `json:"groupId,omitempty"`
	RoomID  string `json:"roomId,omitempty"`
}

type lineWebhookMessage struct {
	ID      string              `json:"id,omitempty"`
	Type    string              `json:"type,omitempty"`
	Text    string              `json:"text,omitempty"`
	Mention *lineWebhookMention `json:"mention,omitempty"`
}

type lineWebhookMention struct {
	Mentionees []lineWebhookMentionee `json:"mentionees,omitempty"`
}

type lineWebhookMentionee struct {
	Type   string `json:"type,omitempty"`
	UserID string `json:"userId,omitempty"`
}

func newLineWebhookHandler(opts lineWebhookHandlerOptions) http.Handler {
	secret := strings.TrimSpace(opts.ChannelSecret)
	allowedGroups := opts.AllowedGroups
	if allowedGroups == nil {
		allowedGroups = map[string]bool{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if opts.Inbound == nil {
			http.Error(w, "line inbound adapter is not initialized", http.StatusInternalServerError)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, lineWebhookBodyMaxBytes))
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		if !verifyLineWebhookSignature(secret, body, r.Header.Get("X-Line-Signature")) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var payload lineWebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		for _, event := range payload.Events {
			inbound, ok, normalizeErr := inboundMessageFromWebhookEvent(event, allowedGroups)
			if normalizeErr != nil {
				logLineWebhookWarn(opts.Logger, "line_webhook_event_invalid",
					"event_id", strings.TrimSpace(event.WebhookEventID),
					"error", normalizeErr.Error(),
				)
				continue
			}
			if !ok {
				continue
			}
			accepted, publishErr := opts.Inbound.HandleInboundMessage(r.Context(), inbound)
			if publishErr != nil {
				logLineWebhookWarn(opts.Logger, "line_webhook_publish_error",
					"event_id", strings.TrimSpace(event.WebhookEventID),
					"chat_id", strings.TrimSpace(inbound.ChatID),
					"message_id", strings.TrimSpace(inbound.MessageID),
					"error", publishErr.Error(),
				)
				continue
			}
			if !accepted {
				logLineWebhookDebug(opts.Logger, "line_webhook_inbound_deduped",
					"chat_id", strings.TrimSpace(inbound.ChatID),
					"message_id", strings.TrimSpace(inbound.MessageID),
				)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
}

func verifyLineWebhookSignature(channelSecret string, body []byte, signature string) bool {
	channelSecret = strings.TrimSpace(channelSecret)
	signature = strings.TrimSpace(signature)
	if channelSecret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(channelSecret))
	_, _ = mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func inboundMessageFromWebhookEvent(event lineWebhookEvent, allowedGroups map[string]bool) (linebus.InboundMessage, bool, error) {
	if strings.ToLower(strings.TrimSpace(event.Type)) != "message" {
		return linebus.InboundMessage{}, false, nil
	}

	chatType := strings.ToLower(strings.TrimSpace(event.Source.Type))
	chatID := ""
	fromUserID := strings.TrimSpace(event.Source.UserID)
	switch chatType {
	case "group":
		chatID = strings.TrimSpace(event.Source.GroupID)
		if chatID == "" {
			return linebus.InboundMessage{}, false, fmt.Errorf("group_id is required")
		}
		if fromUserID == "" {
			return linebus.InboundMessage{}, false, fmt.Errorf("from_user_id is required")
		}
		if len(allowedGroups) > 0 && !allowedGroups[chatID] {
			return linebus.InboundMessage{}, false, nil
		}
	case "user":
		chatType = "private"
		chatID = strings.TrimSpace(event.Source.UserID)
		if chatID == "" {
			return linebus.InboundMessage{}, false, fmt.Errorf("user_id is required")
		}
		if fromUserID == "" {
			fromUserID = chatID
		}
	case "room":
		return linebus.InboundMessage{}, false, nil
	default:
		return linebus.InboundMessage{}, false, nil
	}

	messageID := strings.TrimSpace(event.Message.ID)
	if messageID == "" {
		return linebus.InboundMessage{}, false, fmt.Errorf("message_id is required")
	}

	msgType := strings.ToLower(strings.TrimSpace(event.Message.Type))
	text := strings.TrimSpace(event.Message.Text)
	imagePending := false
	switch msgType {
	case "text":
		if text == "" {
			return linebus.InboundMessage{}, false, nil
		}
	case "image":
		text = "Please process the uploaded image."
		imagePending = true
	default:
		return linebus.InboundMessage{}, false, nil
	}

	return linebus.InboundMessage{
		ChatID:       chatID,
		MessageID:    messageID,
		ReplyToken:   strings.TrimSpace(event.ReplyToken),
		SentAt:       lineEventSentAt(event.Timestamp),
		ChatType:     chatType,
		FromUserID:   fromUserID,
		FromUsername: "",
		DisplayName:  "",
		Text:         text,
		MentionUsers: collectLineMentionUsers(event.Message.Mention),
		ImagePending: imagePending,
		EventID:      strings.TrimSpace(event.WebhookEventID),
	}, true, nil
}

func lineEventSentAt(timestampMS int64) time.Time {
	if timestampMS <= 0 {
		return time.Now().UTC()
	}
	return time.UnixMilli(timestampMS).UTC()
}

func collectLineMentionUsers(mention *lineWebhookMention) []string {
	if mention == nil || len(mention.Mentionees) == 0 {
		return nil
	}
	out := make([]string, 0, len(mention.Mentionees))
	seen := make(map[string]bool, len(mention.Mentionees))
	for _, item := range mention.Mentionees {
		if strings.ToLower(strings.TrimSpace(item.Type)) != "user" {
			continue
		}
		userID := strings.TrimSpace(item.UserID)
		if userID == "" || seen[userID] {
			continue
		}
		seen[userID] = true
		out = append(out, userID)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func logLineWebhookWarn(logger *slog.Logger, msg string, args ...any) {
	if logger == nil {
		return
	}
	logger.Warn(msg, args...)
}

func logLineWebhookDebug(logger *slog.Logger, msg string, args ...any) {
	if logger == nil {
		return
	}
	logger.Debug(msg, args...)
}
