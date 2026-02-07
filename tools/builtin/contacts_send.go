package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/contactsruntime"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/maep"
)

type ContactsSendToolOptions struct {
	Enabled              bool
	ContactsDir          string
	MAEPDir              string
	TelegramBotToken     string
	TelegramBaseURL      string
	AllowHumanSend       bool
	AllowHumanPublicSend bool
}

type ContactsSendTool struct {
	opts ContactsSendToolOptions
}

func NewContactsSendTool(opts ContactsSendToolOptions) *ContactsSendTool {
	return &ContactsSendTool{opts: opts}
}

func (t *ContactsSendTool) Name() string { return "contacts_send" }

func (t *ContactsSendTool) Description() string {
	return "Sends one message to a contact. Routing is automatic: MAEP for agents, Telegram for human contacts."
}

func (t *ContactsSendTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"contact_id": map[string]any{
				"type":        "string",
				"description": "Target contact_id.",
			},
			"topic": map[string]any{
				"type":        "string",
				"description": "Message topic (default share.proactive.v1).",
			},
			"content_type": map[string]any{
				"type":        "string",
				"description": "Payload type. Must be application/json envelope.",
			},
			"message_text": map[string]any{
				"type":        "string",
				"description": "Plain text body; tool wraps it into envelope JSON.",
			},
			"payload_base64": map[string]any{
				"type":        "string",
				"description": "base64url JSON envelope payload when message_text is not used.",
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "UUIDv7 session_id. Required for dialogue topics.",
			},
			"reply_to": map[string]any{
				"type":        "string",
				"description": "Optional reply_to message_id.",
			},
			"source_chat_id": map[string]any{
				"type":        "integer",
				"description": "Optional Telegram routing hint.",
			},
			"source_chat_type": map[string]any{
				"type":        "string",
				"description": "Optional Telegram routing hint: private|group|supergroup.",
			},
		},
		"required": []string{"contact_id"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ContactsSendTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || !t.opts.Enabled {
		return "", fmt.Errorf("contacts_send tool is disabled")
	}
	contactsDir := pathutil.ExpandHomePath(strings.TrimSpace(t.opts.ContactsDir))
	if contactsDir == "" {
		return "", fmt.Errorf("contacts dir is not configured")
	}
	contactID, _ := params["contact_id"].(string)
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return "", fmt.Errorf("missing required param: contact_id")
	}
	topic, _ := params["topic"].(string)
	topic = strings.TrimSpace(topic)
	if topic == "" {
		topic = "share.proactive.v1"
	}

	contentType, payload, err := resolveSendPayload(params, topic, time.Now().UTC())
	if err != nil {
		return "", err
	}

	sender, err := contactsruntime.NewRoutingSender(ctx, contactsruntime.SenderOptions{
		MAEPDir:              strings.TrimSpace(t.opts.MAEPDir),
		TelegramBotToken:     strings.TrimSpace(t.opts.TelegramBotToken),
		TelegramBaseURL:      strings.TrimSpace(t.opts.TelegramBaseURL),
		AllowHumanSend:       t.opts.AllowHumanSend,
		AllowHumanPublicSend: t.opts.AllowHumanPublicSend,
	})
	if err != nil {
		return "", err
	}
	defer sender.Close()

	sourceChatType, _ := params["source_chat_type"].(string)
	sourceChatID := int64(parseIntDefault(params["source_chat_id"], 0))

	decision := contacts.ShareDecision{
		ContactID:      contactID,
		Topic:          topic,
		ContentType:    contentType,
		PayloadBase64:  payload,
		SourceChatID:   sourceChatID,
		SourceChatType: strings.TrimSpace(sourceChatType),
	}
	decision.ItemID = "manual_" + uuid.NewString()
	decision.IdempotencyKey = "manual:" + uuid.NewString()

	svc := contacts.NewService(contacts.NewFileStore(contactsDir))
	outcome, err := svc.SendDecision(ctx, time.Now().UTC(), decision, sender)
	if err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(map[string]any{
		"outcome": outcome,
	}, "", "  ")
	return string(out), nil
}

func resolveSendPayload(params map[string]any, topic string, now time.Time) (string, string, error) {
	sessionID, _ := params["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		parsed, err := uuid.Parse(sessionID)
		if err != nil || parsed.Version() != uuid.Version(7) {
			return "", "", fmt.Errorf("session_id must be uuid_v7")
		}
	}
	replyTo, _ := params["reply_to"].(string)
	replyTo = strings.TrimSpace(replyTo)

	if text, ok := params["message_text"].(string); ok {
		text = strings.TrimSpace(text)
		if text != "" {
			if maep.IsDialogueTopic(topic) && sessionID == "" {
				return "", "", fmt.Errorf("session_id is required for dialogue topics")
			}
			envelope := map[string]any{
				"message_id": "msg_" + uuid.NewString(),
				"text":       text,
				"sent_at":    now.UTC().Format(time.RFC3339),
			}
			if sessionID != "" {
				envelope["session_id"] = sessionID
			}
			if replyTo != "" {
				envelope["reply_to"] = replyTo
			}
			raw, err := json.Marshal(envelope)
			if err != nil {
				return "", "", err
			}
			return "application/json", base64.RawURLEncoding.EncodeToString(raw), nil
		}
	}

	if raw, ok := params["payload_base64"].(string); ok {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			contentType, _ := params["content_type"].(string)
			contentType = strings.TrimSpace(contentType)
			if contentType == "" {
				contentType = "application/json"
			}
			if !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
				return "", "", fmt.Errorf("content_type must be application/json envelope")
			}
			payloadBytes, err := base64.RawURLEncoding.DecodeString(raw)
			if err != nil {
				return "", "", fmt.Errorf("payload_base64 decode failed: %w", err)
			}
			var envelope map[string]any
			if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
				return "", "", fmt.Errorf("payload_base64 must be envelope json")
			}
			if _, ok := envelope["message_id"].(string); !ok {
				return "", "", fmt.Errorf("payload envelope missing message_id")
			}
			if _, ok := envelope["text"].(string); !ok {
				return "", "", fmt.Errorf("payload envelope missing text")
			}
			sentAt, ok := envelope["sent_at"].(string)
			if !ok || strings.TrimSpace(sentAt) == "" {
				return "", "", fmt.Errorf("payload envelope missing sent_at")
			}
			if _, err := time.Parse(time.RFC3339, strings.TrimSpace(sentAt)); err != nil {
				return "", "", fmt.Errorf("payload envelope sent_at must be RFC3339")
			}
			if maep.IsDialogueTopic(topic) {
				sessionRaw, _ := envelope["session_id"].(string)
				sessionRaw = strings.TrimSpace(sessionRaw)
				if sessionRaw == "" {
					return "", "", fmt.Errorf("payload envelope missing session_id for dialogue topic")
				}
				parsed, err := uuid.Parse(sessionRaw)
				if err != nil || parsed.Version() != uuid.Version(7) {
					return "", "", fmt.Errorf("payload envelope session_id must be uuid_v7")
				}
			}
			return "application/json", raw, nil
		}
	}
	return "", "", fmt.Errorf("message_text or payload_base64 is required")
}
