package contactsruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/maep"
)

const defaultTelegramBaseURL = "https://api.telegram.org"

type SenderOptions struct {
	MAEPDir              string
	TelegramBotToken     string
	TelegramBaseURL      string
	AllowHumanSend       bool
	AllowHumanPublicSend bool
	Logger               *slog.Logger
}

type RoutingSender struct {
	maepNode             *maep.Node
	telegramClient       *http.Client
	telegramBaseURL      string
	telegramBotToken     string
	allowHumanSend       bool
	allowHumanPublicSend bool
}

func NewRoutingSender(ctx context.Context, opts SenderOptions) (*RoutingSender, error) {
	dir := strings.TrimSpace(opts.MAEPDir)
	if dir == "" {
		dir = statepaths.MAEPDir()
	} else {
		dir = pathutil.ExpandHomePath(dir)
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}

	svc := maep.NewService(maep.NewFileStore(dir))
	node, err := maep.NewNode(ctx, svc, maep.NodeOptions{DialOnly: true, Logger: logger})
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimSpace(opts.TelegramBaseURL)
	if baseURL == "" {
		baseURL = defaultTelegramBaseURL
	}

	return &RoutingSender{
		maepNode:             node,
		telegramClient:       &http.Client{Timeout: 30 * time.Second},
		telegramBaseURL:      baseURL,
		telegramBotToken:     strings.TrimSpace(opts.TelegramBotToken),
		allowHumanSend:       opts.AllowHumanSend,
		allowHumanPublicSend: opts.AllowHumanPublicSend,
	}, nil
}

func (s *RoutingSender) Close() error {
	if s == nil || s.maepNode == nil {
		return nil
	}
	return s.maepNode.Close()
}

func (s *RoutingSender) Send(ctx context.Context, contact contacts.Contact, decision contacts.ShareDecision) (bool, bool, error) {
	if s == nil {
		return false, false, fmt.Errorf("nil routing sender")
	}
	if target, resolvedChatType, err := ResolveTelegramTarget(contact, decision); err == nil && target != nil {
		if !s.allowHumanSend {
			return false, false, fmt.Errorf("human proactive send is disabled by config")
		}
		if !s.allowHumanPublicSend && IsPublicTelegramTarget(contact, decision, target, resolvedChatType) {
			return false, false, fmt.Errorf("public human proactive send is disabled by config")
		}
		return s.sendTelegram(ctx, target, decision)
	}
	return s.sendMAEP(ctx, contact, decision)
}

func (s *RoutingSender) sendMAEP(ctx context.Context, contact contacts.Contact, decision contacts.ShareDecision) (bool, bool, error) {
	if s == nil || s.maepNode == nil {
		return false, false, fmt.Errorf("maep sender not configured")
	}
	req, err := buildMAEPDataPushRequest(decision, time.Now().UTC())
	if err != nil {
		return false, false, err
	}
	result, err := s.maepNode.PushData(ctx, strings.TrimSpace(decision.PeerID), normalizeStrings(contact.Addresses), req, false)
	if err != nil {
		return false, false, err
	}
	return result.Accepted, result.Deduped, nil
}

func buildMAEPDataPushRequest(decision contacts.ShareDecision, now time.Time) (maep.DataPushRequest, error) {
	now = now.UTC()
	req := maep.DataPushRequest{
		Topic:          strings.TrimSpace(decision.Topic),
		ContentType:    strings.TrimSpace(decision.ContentType),
		PayloadBase64:  strings.TrimSpace(decision.PayloadBase64),
		IdempotencyKey: strings.TrimSpace(decision.IdempotencyKey),
	}
	if req.Topic == "" {
		req.Topic = "share.proactive.v1"
	}
	envelopePayload, err := buildEnvelopePayload(decision, req.Topic, req.ContentType, req.PayloadBase64, now)
	if err != nil {
		return maep.DataPushRequest{}, err
	}
	req.ContentType = "application/json"
	req.PayloadBase64 = base64.RawURLEncoding.EncodeToString(envelopePayload)
	return req, nil
}

func buildEnvelopePayload(decision contacts.ShareDecision, topic string, contentType string, payloadBase64 string, now time.Time) ([]byte, error) {
	text, extras, err := decodeEnvelopeTextAndExtras(contentType, payloadBase64)
	if err != nil {
		return nil, err
	}
	messageID := strings.TrimSpace(decision.ItemID)
	if messageID == "" {
		messageID = "msg_" + uuid.NewString()
	}
	payload := map[string]any{
		"message_id": messageID,
		"text":       text,
		"sent_at":    now.Format(time.RFC3339),
	}
	sessionID := strings.TrimSpace(extras["session_id"])
	if maep.IsDialogueTopic(topic) && sessionID == "" {
		return nil, fmt.Errorf("session_id is required for dialogue topics")
	}
	if sessionID != "" {
		if err := validateSessionID(sessionID); err != nil {
			return nil, err
		}
		payload["session_id"] = sessionID
	}
	if replyTo := strings.TrimSpace(extras["reply_to"]); replyTo != "" {
		payload["reply_to"] = replyTo
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal envelope payload: %w", err)
	}
	return raw, nil
}

func validateSessionID(sessionID string) error {
	id, err := uuid.Parse(strings.TrimSpace(sessionID))
	if err != nil {
		return fmt.Errorf("session_id must be uuid_v7")
	}
	if id.Version() != uuid.Version(7) {
		return fmt.Errorf("session_id must be uuid_v7")
	}
	return nil
}

func decodeEnvelopeTextAndExtras(contentType string, payloadBase64 string) (string, map[string]string, error) {
	payloadBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(payloadBase64))
	if err != nil {
		return "", nil, fmt.Errorf("decode payload_base64: %w", err)
	}
	extras := map[string]string{}
	lowerType := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(lowerType, "application/json") {
		var obj map[string]any
		if err := json.Unmarshal(payloadBytes, &obj); err == nil {
			for _, key := range []string{"text", "message", "content", "prompt"} {
				if v, ok := obj[key].(string); ok && strings.TrimSpace(v) != "" {
					if session, ok := obj["session_id"].(string); ok {
						extras["session_id"] = strings.TrimSpace(session)
					}
					if replyTo, ok := obj["reply_to"].(string); ok {
						extras["reply_to"] = strings.TrimSpace(replyTo)
					}
					return strings.TrimSpace(v), extras, nil
				}
			}
			normalized, err := json.Marshal(obj)
			if err == nil {
				return strings.TrimSpace(string(normalized)), extras, nil
			}
		}
	}
	text := strings.TrimSpace(string(payloadBytes))
	if text == "" {
		text = "(empty)"
	}
	return text, extras, nil
}

func (s *RoutingSender) sendTelegram(ctx context.Context, target any, decision contacts.ShareDecision) (bool, bool, error) {
	if s == nil || strings.TrimSpace(s.telegramBotToken) == "" {
		return false, false, fmt.Errorf("telegram sender not configured")
	}
	text, _, err := decodeEnvelopeTextAndExtras(decision.ContentType, decision.PayloadBase64)
	if err != nil {
		return false, false, err
	}

	body := map[string]any{
		"chat_id":                  target,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	raw, _ := json.Marshal(body)

	url := strings.TrimRight(strings.TrimSpace(s.telegramBaseURL), "/") + "/bot" + strings.TrimSpace(s.telegramBotToken) + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(raw)))
	if err != nil {
		return false, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.telegramClient.Do(req)
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()
	respRaw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, false, fmt.Errorf("telegram http %d: %s", resp.StatusCode, strings.TrimSpace(string(respRaw)))
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(respRaw, &out); err != nil {
		return false, false, fmt.Errorf("decode telegram response: %w", err)
	}
	if !out.OK {
		return false, false, fmt.Errorf("telegram sendMessage: ok=false")
	}
	return true, false, nil
}

func ResolveTelegramTarget(contact contacts.Contact, decision contacts.ShareDecision) (any, string, error) {
	if chatID, chatType, ok := preferredChatByDecision(contact, decision); ok {
		return chatID, chatType, nil
	}
	for _, raw := range []string{contact.SubjectID, contact.ContactID} {
		value := strings.TrimSpace(raw)
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "tg:id:") {
			idText := strings.TrimSpace(value[len("tg:id:"):])
			chatID, err := strconv.ParseInt(idText, 10, 64)
			if err != nil {
				return nil, "", fmt.Errorf("invalid telegram id in %q", raw)
			}
			return chatID, "private", nil
		}
		if strings.HasPrefix(lower, "tg:@") {
			username := strings.TrimSpace(value[len("tg:@"):])
			username = strings.TrimPrefix(username, "@")
			if username == "" {
				return nil, "", fmt.Errorf("empty telegram username in %q", raw)
			}
			return "@" + username, "", nil
		}
	}
	return nil, "", fmt.Errorf("telegram target not found in subject_id/contact_id")
}

func IsPublicTelegramTarget(contact contacts.Contact, decision contacts.ShareDecision, target any, resolvedChatType string) bool {
	chatType := strings.ToLower(strings.TrimSpace(resolvedChatType))
	if chatType == "group" || chatType == "supergroup" {
		return true
	}
	if chatType == "private" {
		return false
	}
	if id, ok := target.(int64); ok && id < 0 {
		return true
	}
	if decision.SourceChatID != 0 {
		for _, item := range contact.TelegramChats {
			if item.ChatID != decision.SourceChatID {
				continue
			}
			t := strings.ToLower(strings.TrimSpace(item.ChatType))
			return t == "group" || t == "supergroup"
		}
	}
	t := strings.ToLower(strings.TrimSpace(decision.SourceChatType))
	return t == "group" || t == "supergroup"
}

func preferredChatByDecision(contact contacts.Contact, decision contacts.ShareDecision) (int64, string, bool) {
	chats := contact.TelegramChats
	if len(chats) == 0 {
		return 0, "", false
	}
	if decision.SourceChatID != 0 {
		for _, item := range chats {
			if item.ChatID == decision.SourceChatID {
				return item.ChatID, strings.ToLower(strings.TrimSpace(item.ChatType)), true
			}
		}
	}
	t := strings.ToLower(strings.TrimSpace(decision.SourceChatType))
	if t != "" {
		for _, item := range chats {
			if strings.ToLower(strings.TrimSpace(item.ChatType)) == t {
				return item.ChatID, t, true
			}
		}
	}
	for _, item := range chats {
		if strings.ToLower(strings.TrimSpace(item.ChatType)) == "private" {
			return item.ChatID, "private", true
		}
	}
	for _, item := range chats {
		if item.ChatID != 0 {
			return item.ChatID, strings.ToLower(strings.TrimSpace(item.ChatType)), true
		}
	}
	return 0, "", false
}

func normalizeStrings(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
