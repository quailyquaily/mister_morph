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
)

const (
	contactsSendContentType = "application/json"
)

type ContactsSendToolOptions struct {
	Enabled          bool
	ContactsDir      string
	TelegramBotToken string
	TelegramBaseURL  string
	SlackBotToken    string
	SlackBaseURL     string
	LineChannelToken string
	LineBaseURL      string
	LarkAppID        string
	LarkAppSecret    string
	LarkBaseURL      string
	FailureCooldown  time.Duration
}

type ContactsSendTool struct {
	opts ContactsSendToolOptions
}

func NewContactsSendTool(opts ContactsSendToolOptions) *ContactsSendTool {
	return &ContactsSendTool{opts: opts}
}

func (t *ContactsSendTool) Name() string { return "contacts_send" }

func (t *ContactsSendTool) Description() string {
	return "Sends a message to a contact. Routing is automatic across Slack, Telegram, LINE, and Lark based on chat_id/contact reachability." +
		"NEVER send message to people who is talking with you."
}

func (t *ContactsSendTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"contact_id": map[string]any{
				"type":        "string",
				"description": "Target contact_id. e.g.: slack:<team_id>:<user_id>, tg:@<username>, tg:<chat_id>, line_user:<user_id>, line:<chat_id>, lark_user:<open_id>, lark:<chat_id>. NEVER send message to people who is talking with you.",
			},
			"message_text": map[string]any{
				"type":        "string",
				"description": "Plain text body; tool wraps it into envelope JSON.",
			},
			"message_base64": map[string]any{
				"type":        "string",
				"description": "Optional base64 JSON envelope payload when message_text is not used.",
			},
			"chat_id": map[string]any{
				"type":        "string",
				"description": "Optional chat id hint. e.g. slack:<team_id>:<channel_id>, tg:<chat_id>, line:<chat_id>, or lark:<chat_id>.",
			},
			"content_type": map[string]any{
				"type":        "string",
				"description": "Optional Payload type (default application/json).",
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "Optional UUIDv7 session_id; auto-generated when omitted.",
			},
			"reply_to": map[string]any{
				"type":        "string",
				"description": "Optional reply_to message_id.",
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
	contactID, _ := params["contact_id"].(string)
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return "", fmt.Errorf("missing required param: contact_id")
	}
	chatID, err := parseContactsSendChatID(params)
	if err != nil {
		return "", err
	}
	if runtimeCtx, ok := ContactsSendRuntimeContextFromContext(ctx); ok {
		if field, target, blocked := contactsSendBlockedTarget(contactID, chatID, runtimeCtx); blocked {
			return "", fmt.Errorf("contacts_send blocked: %s %q matches current conversation counterpart", field, target)
		}
	}
	contactsDir := pathutil.ExpandHomePath(strings.TrimSpace(t.opts.ContactsDir))
	if contactsDir == "" {
		return "", fmt.Errorf("contacts dir is not configured")
	}

	contentType, payload, err := resolveSendPayload(params, time.Now().UTC())
	if err != nil {
		return "", err
	}

	sender, err := contactsruntime.NewRoutingSender(ctx, contactsruntime.SenderOptions{
		TelegramBotToken: strings.TrimSpace(t.opts.TelegramBotToken),
		TelegramBaseURL:  strings.TrimSpace(t.opts.TelegramBaseURL),
		SlackBotToken:    strings.TrimSpace(t.opts.SlackBotToken),
		SlackBaseURL:     strings.TrimSpace(t.opts.SlackBaseURL),
		LineChannelToken: strings.TrimSpace(t.opts.LineChannelToken),
		LineBaseURL:      strings.TrimSpace(t.opts.LineBaseURL),
		LarkAppID:        strings.TrimSpace(t.opts.LarkAppID),
		LarkAppSecret:    strings.TrimSpace(t.opts.LarkAppSecret),
		LarkBaseURL:      strings.TrimSpace(t.opts.LarkBaseURL),
	})
	if err != nil {
		return "", err
	}
	defer sender.Close()

	decision := contacts.ShareDecision{
		ContactID:     contactID,
		ChatID:        chatID,
		ContentType:   contentType,
		PayloadBase64: payload,
	}
	decision.ItemID = "manual_" + uuid.NewString()
	decision.IdempotencyKey = "manual:" + uuid.NewString()

	svc := contacts.NewServiceWithOptions(
		contacts.NewFileStore(contactsDir),
		contacts.ServiceOptions{
			FailureCooldown: t.opts.FailureCooldown,
		},
	)
	outcome, err := svc.SendDecision(ctx, time.Now().UTC(), decision, sender)
	if err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(map[string]any{
		"outcome": outcome,
	}, "", "  ")
	return string(out), nil
}

func parseContactsSendChatID(params map[string]any) (string, error) {
	raw, exists := params["chat_id"]
	if !exists || raw == nil {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("chat_id must be a string")
	}
	return strings.TrimSpace(value), nil
}

func resolveSendPayload(params map[string]any, now time.Time) (string, string, error) {
	contentType, _ := params["content_type"].(string)
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		contentType = contactsSendContentType
	}
	if !strings.HasPrefix(strings.ToLower(contentType), contactsSendContentType) {
		return "", "", fmt.Errorf("content_type must be application/json envelope")
	}

	sessionID, _ := params["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		if err := validateUUIDv7SessionID(sessionID); err != nil {
			return "", "", fmt.Errorf("session_id must be uuid_v7")
		}
	}
	replyTo, _ := params["reply_to"].(string)
	replyTo = strings.TrimSpace(replyTo)

	if text, ok := params["message_text"].(string); ok {
		text = strings.TrimSpace(text)
		if text != "" {
			resolvedSessionID := sessionID
			if resolvedSessionID == "" {
				generatedSessionID, err := generateUUIDv7SessionID()
				if err != nil {
					return "", "", err
				}
				resolvedSessionID = generatedSessionID
			}
			envelope := map[string]any{
				"message_id": "msg_" + uuid.NewString(),
				"text":       text,
				"sent_at":    now.UTC().Format(time.RFC3339),
				"session_id": resolvedSessionID,
			}
			if replyTo != "" {
				envelope["reply_to"] = replyTo
			}
			raw, err := json.Marshal(envelope)
			if err != nil {
				return "", "", err
			}
			return contentType, base64.RawURLEncoding.EncodeToString(raw), nil
		}
	}

	if raw, ok := params["message_base64"].(string); ok {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			payloadBytes, err := base64.RawURLEncoding.DecodeString(raw)
			if err != nil {
				return "", "", fmt.Errorf("message_base64 decode failed: %w", err)
			}
			var envelope map[string]any
			if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
				return "", "", fmt.Errorf("message_base64 must be envelope json")
			}
			if _, ok := envelope["message_id"].(string); !ok {
				return "", "", fmt.Errorf("message envelope missing message_id")
			}
			if _, ok := envelope["text"].(string); !ok {
				return "", "", fmt.Errorf("message envelope missing text")
			}
			sentAt, ok := envelope["sent_at"].(string)
			if !ok || strings.TrimSpace(sentAt) == "" {
				return "", "", fmt.Errorf("message envelope missing sent_at")
			}
			if _, err := time.Parse(time.RFC3339, strings.TrimSpace(sentAt)); err != nil {
				return "", "", fmt.Errorf("message envelope sent_at must be RFC3339")
			}
			sessionRaw, _ := envelope["session_id"].(string)
			sessionRaw = strings.TrimSpace(sessionRaw)
			if sessionRaw == "" {
				sessionRaw = sessionID
			}
			if sessionRaw == "" {
				generatedSessionID, err := generateUUIDv7SessionID()
				if err != nil {
					return "", "", err
				}
				sessionRaw = generatedSessionID
			}
			if err := validateUUIDv7SessionID(sessionRaw); err != nil {
				return "", "", fmt.Errorf("message envelope session_id must be uuid_v7")
			}
			envelope["session_id"] = sessionRaw
			normalizedRaw, err := json.Marshal(envelope)
			if err != nil {
				return "", "", fmt.Errorf("marshal message envelope: %w", err)
			}
			return contentType, base64.RawURLEncoding.EncodeToString(normalizedRaw), nil
		}
	}
	return "", "", fmt.Errorf("message_text or message_base64 is required")
}

func validateUUIDv7SessionID(sessionID string) error {
	parsed, err := uuid.Parse(strings.TrimSpace(sessionID))
	if err != nil || parsed.Version() != uuid.Version(7) {
		return fmt.Errorf("session_id must be uuid_v7")
	}
	return nil
}

func generateUUIDv7SessionID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate session_id: %w", err)
	}
	return id.String(), nil
}
