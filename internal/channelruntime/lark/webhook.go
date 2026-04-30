package lark

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
)

const larkWebhookBodyMaxBytes = 1 << 20 // 1MB

const (
	larkRequestNonceHeader     = "X-Lark-Request-Nonce"
	larkRequestTimestampHeader = "X-Lark-Request-Timestamp"
	larkRequestSignatureHeader = "X-Lark-Signature"
)

type larkWebhookHandlerOptions struct {
	VerificationToken string
	EncryptKey        string
	Inbound           *larkbus.InboundAdapter
	AllowedChats      map[string]bool
	API               *larkAPI
	FileCacheDir      string
	ImageRecognition  bool
	Logger            *slog.Logger
}

type larkWebhookEnvelope struct {
	Encrypt   string             `json:"encrypt,omitempty"`
	Type      string             `json:"type,omitempty"`
	Challenge string             `json:"challenge,omitempty"`
	Token     string             `json:"token,omitempty"`
	Schema    string             `json:"schema,omitempty"`
	Header    *larkWebhookHeader `json:"header,omitempty"`
	Event     *larkWebhookEvent  `json:"event,omitempty"`
}

type larkWebhookHeader struct {
	EventID    string `json:"event_id,omitempty"`
	EventType  string `json:"event_type,omitempty"`
	CreateTime string `json:"create_time,omitempty"`
	Token      string `json:"token,omitempty"`
	AppID      string `json:"app_id,omitempty"`
	TenantKey  string `json:"tenant_key,omitempty"`
}

type larkWebhookEvent struct {
	Sender  larkWebhookSender  `json:"sender,omitempty"`
	Message larkWebhookMessage `json:"message,omitempty"`
}

type larkWebhookSender struct {
	SenderID   larkWebhookUserID `json:"sender_id,omitempty"`
	SenderType string            `json:"sender_type,omitempty"`
	TenantKey  string            `json:"tenant_key,omitempty"`
}

type larkWebhookUserID struct {
	UnionID string `json:"union_id,omitempty"`
	UserID  string `json:"user_id,omitempty"`
	OpenID  string `json:"open_id,omitempty"`
}

type larkWebhookMessage struct {
	MessageID   string                    `json:"message_id,omitempty"`
	RootID      string                    `json:"root_id,omitempty"`
	ParentID    string                    `json:"parent_id,omitempty"`
	CreateTime  string                    `json:"create_time,omitempty"`
	UpdateTime  string                    `json:"update_time,omitempty"`
	ChatID      string                    `json:"chat_id,omitempty"`
	ThreadID    string                    `json:"thread_id,omitempty"`
	ChatType    string                    `json:"chat_type,omitempty"`
	MessageType string                    `json:"message_type,omitempty"`
	Content     string                    `json:"content,omitempty"`
	Mentions    []larkWebhookMentionEvent `json:"mentions,omitempty"`
	UserAgent   string                    `json:"user_agent,omitempty"`
}

type larkWebhookMentionEvent struct {
	Key       string            `json:"key,omitempty"`
	ID        larkWebhookUserID `json:"id,omitempty"`
	Name      string            `json:"name,omitempty"`
	TenantKey string            `json:"tenant_key,omitempty"`
}

type larkTextContent struct {
	Text string `json:"text,omitempty"`
}

type larkImageContent struct {
	ImageKey string `json:"image_key,omitempty"`
	Text     string `json:"text,omitempty"`
}

type larkParsedInboundMessage struct {
	Message   larkbus.InboundMessage
	ImageKeys []string
}

func newLarkWebhookHandler(opts larkWebhookHandlerOptions) http.Handler {
	verificationToken := strings.TrimSpace(opts.VerificationToken)
	encryptKey := strings.TrimSpace(opts.EncryptKey)
	allowedChats := opts.AllowedChats
	if allowedChats == nil {
		allowedChats = map[string]bool{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if opts.Inbound == nil {
			http.Error(w, "lark inbound adapter is not initialized", http.StatusInternalServerError)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, larkWebhookBodyMaxBytes))
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		plainBody, reqType, eventType, token, challenge, err := parseLarkWebhookRequest(body, r.Header, encryptKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if verificationToken != "" && token != verificationToken {
			http.Error(w, "invalid verification token", http.StatusUnauthorized)
			return
		}
		if reqType == "url_verification" {
			writeLarkJSON(w, http.StatusOK, map[string]string{"challenge": challenge})
			return
		}
		if eventType != "im.message.receive_v1" {
			writeLarkJSON(w, http.StatusOK, map[string]any{})
			return
		}
		var payload larkWebhookEnvelope
		if err := json.Unmarshal(plainBody, &payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		parsed, ok, normalizeErr := inboundMessageFromWebhookEvent(payload, allowedChats)
		if normalizeErr != nil {
			logLarkWebhookWarn(opts.Logger, "lark_webhook_event_invalid",
				"event_id", strings.TrimSpace(payload.Header.GetEventID()),
				"error", normalizeErr.Error(),
			)
			writeLarkJSON(w, http.StatusOK, map[string]any{})
			return
		}
		if ok {
			inbound := parsed.Message
			if len(parsed.ImageKeys) > 0 {
				inbound.Text = larkImageFallbackText(inbound.Text, opts.ImageRecognition, len(parsed.ImageKeys))
				if opts.ImageRecognition {
					for _, imageKey := range parsed.ImageKeys {
						if len(inbound.ImagePaths) >= larkLLMMaxImages {
							break
						}
						imageCtx, cancelImage := larkImageDownloadContext(r.Context())
						path, imageErr := downloadLarkImageToCache(imageCtx, opts.API, strings.TrimSpace(opts.FileCacheDir), inbound.MessageID, imageKey, larkLLMMaxImageBytes)
						cancelImage()
						if imageErr != nil {
							logLarkWebhookWarn(opts.Logger, "lark_image_download_failed",
								"event_id", strings.TrimSpace(payload.Header.GetEventID()),
								"chat_id", strings.TrimSpace(inbound.ChatID),
								"message_id", strings.TrimSpace(inbound.MessageID),
								"image_key", strings.TrimSpace(imageKey),
								"error", imageErr.Error(),
							)
							continue
						}
						inbound.ImagePaths = append(inbound.ImagePaths, path)
					}
					if len(inbound.ImagePaths) == 0 {
						inbound.Text = appendLarkImageReadFailure(inbound.Text)
					}
				}
			}
			accepted, publishErr := opts.Inbound.HandleInboundMessage(r.Context(), inbound)
			if publishErr != nil {
				logLarkWebhookWarn(opts.Logger, "lark_webhook_publish_error",
					"event_id", strings.TrimSpace(payload.Header.GetEventID()),
					"chat_id", strings.TrimSpace(inbound.ChatID),
					"message_id", strings.TrimSpace(inbound.MessageID),
					"error", publishErr.Error(),
				)
			} else if !accepted {
				logLarkWebhookDebug(opts.Logger, "lark_webhook_inbound_deduped",
					"chat_id", strings.TrimSpace(inbound.ChatID),
					"message_id", strings.TrimSpace(inbound.MessageID),
				)
			}
		}
		writeLarkJSON(w, http.StatusOK, map[string]any{})
	})
}

func parseLarkWebhookRequest(body []byte, headers http.Header, encryptKey string) ([]byte, string, string, string, string, error) {
	plainBody := body
	if strings.TrimSpace(encryptKey) != "" {
		var encrypted struct {
			Encrypt string `json:"encrypt,omitempty"`
		}
		if err := json.Unmarshal(body, &encrypted); err != nil {
			return nil, "", "", "", "", fmt.Errorf("invalid encrypted payload")
		}
		if strings.TrimSpace(encrypted.Encrypt) == "" {
			return nil, "", "", "", "", fmt.Errorf("encrypted message is blank")
		}
		decrypted, err := decryptLarkWebhook(encrypted.Encrypt, encryptKey)
		if err != nil {
			return nil, "", "", "", "", err
		}
		plainBody = decrypted
	}
	var fuzzy larkWebhookEnvelope
	if err := json.Unmarshal(plainBody, &fuzzy); err != nil {
		return nil, "", "", "", "", fmt.Errorf("invalid webhook payload")
	}
	reqType := strings.TrimSpace(fuzzy.Type)
	eventType := ""
	token := strings.TrimSpace(fuzzy.Token)
	challenge := strings.TrimSpace(fuzzy.Challenge)
	if fuzzy.Header != nil {
		token = strings.TrimSpace(fuzzy.Header.Token)
		eventType = strings.TrimSpace(fuzzy.Header.EventType)
	}
	if strings.TrimSpace(encryptKey) != "" && reqType != "url_verification" {
		if err := verifyLarkWebhookSignature(headers, encryptKey, body); err != nil {
			return nil, "", "", "", "", err
		}
	}
	return plainBody, reqType, eventType, token, challenge, nil
}

func decryptLarkWebhook(encrypt, secret string) ([]byte, error) {
	buf, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encrypt))
	if err != nil {
		return nil, fmt.Errorf("lark decrypt base64: %w", err)
	}
	if len(buf) < aes.BlockSize {
		return nil, fmt.Errorf("lark decrypt: cipher too short")
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:sha256.Size])
	if err != nil {
		return nil, fmt.Errorf("lark decrypt cipher: %w", err)
	}
	iv := buf[:aes.BlockSize]
	buf = buf[aes.BlockSize:]
	if len(buf)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("lark decrypt: invalid block size")
	}
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(buf, buf)
	start := strings.Index(string(buf), "{")
	if start < 0 {
		start = 0
	}
	end := strings.LastIndex(string(buf), "}")
	if end < 0 {
		end = len(buf) - 1
	}
	if start > end || end >= len(buf) {
		return nil, fmt.Errorf("lark decrypt: invalid payload")
	}
	return buf[start : end+1], nil
}

func verifyLarkWebhookSignature(headers http.Header, encryptKey string, body []byte) error {
	timestamp := strings.TrimSpace(headers.Get(larkRequestTimestampHeader))
	nonce := strings.TrimSpace(headers.Get(larkRequestNonceHeader))
	signature := strings.TrimSpace(headers.Get(larkRequestSignatureHeader))
	if timestamp == "" || nonce == "" || signature == "" {
		return fmt.Errorf("missing lark signature headers")
	}
	h := sha256.New()
	_, _ = h.Write([]byte(timestamp + nonce + encryptKey + string(body)))
	expected := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(expected, signature) {
		return fmt.Errorf("invalid lark signature")
	}
	return nil
}

func inboundMessageFromWebhookEvent(payload larkWebhookEnvelope, allowedChats map[string]bool) (larkParsedInboundMessage, bool, error) {
	if payload.Event == nil {
		return larkParsedInboundMessage{}, false, nil
	}
	event := payload.Event
	if !strings.EqualFold(strings.TrimSpace(event.Sender.SenderType), "user") {
		return larkParsedInboundMessage{}, false, nil
	}
	chatID := strings.TrimSpace(event.Message.ChatID)
	if chatID == "" {
		return larkParsedInboundMessage{}, false, fmt.Errorf("chat_id is required")
	}
	if len(allowedChats) > 0 && !allowedChats[chatID] {
		return larkParsedInboundMessage{}, false, nil
	}
	messageID := strings.TrimSpace(event.Message.MessageID)
	if messageID == "" {
		return larkParsedInboundMessage{}, false, fmt.Errorf("message_id is required")
	}
	chatType, err := normalizeLarkInboundChatType(event.Message.ChatType)
	if err != nil {
		return larkParsedInboundMessage{}, false, err
	}
	fromUserID := strings.TrimSpace(event.Sender.SenderID.OpenID)
	if fromUserID == "" {
		return larkParsedInboundMessage{}, false, fmt.Errorf("from_user_id is required")
	}
	text, imageKeys, ok, err := extractLarkMessageContent(event.Message.MessageType, event.Message.Content)
	if err != nil {
		return larkParsedInboundMessage{}, false, err
	}
	if !ok {
		return larkParsedInboundMessage{}, false, nil
	}
	return larkParsedInboundMessage{
		Message: larkbus.InboundMessage{
			ChatID:       chatID,
			MessageID:    messageID,
			SentAt:       parseLarkEventTime(event.Message.CreateTime),
			ChatType:     chatType,
			FromUserID:   fromUserID,
			DisplayName:  "",
			Text:         text,
			MentionUsers: collectLarkMentionUsers(event.Message.Mentions),
			EventID:      strings.TrimSpace(payload.Header.GetEventID()),
		},
		ImageKeys: imageKeys,
	}, true, nil
}

func normalizeLarkInboundChatType(raw string) (string, error) {
	chatType := strings.ToLower(strings.TrimSpace(raw))
	switch chatType {
	case "group", "topic_group":
		return "group", nil
	case "p2p", "private":
		return "private", nil
	case "":
		return "", fmt.Errorf("chat_type is required")
	default:
		return "", fmt.Errorf("unsupported lark chat_type: %s", chatType)
	}
}

func extractLarkMessageContent(messageType, content string) (string, []string, bool, error) {
	messageType = strings.ToLower(strings.TrimSpace(messageType))
	content = strings.TrimSpace(content)
	if content == "" {
		return "", nil, false, nil
	}
	switch messageType {
	case "text":
		var textContent larkTextContent
		if err := json.Unmarshal([]byte(content), &textContent); err != nil {
			return "", nil, false, fmt.Errorf("invalid text content")
		}
		text := strings.TrimSpace(textContent.Text)
		if text == "" {
			return "", nil, false, nil
		}
		return text, nil, true, nil
	case "image":
		var imageContent larkImageContent
		if err := json.Unmarshal([]byte(content), &imageContent); err != nil {
			return "", nil, false, fmt.Errorf("invalid image content")
		}
		imageKey := strings.TrimSpace(imageContent.ImageKey)
		if imageKey == "" {
			return "", nil, false, nil
		}
		text := strings.TrimSpace(imageContent.Text)
		if text == "" {
			text = "User sent an image."
		}
		return text, []string{imageKey}, true, nil
	default:
		return "", nil, false, nil
	}
}

func collectLarkMentionUsers(items []larkWebhookMentionEvent) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		openID := strings.TrimSpace(item.ID.OpenID)
		if openID == "" || seen[openID] {
			continue
		}
		seen[openID] = true
		out = append(out, openID)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseLarkEventTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC()
	}
	ms, err := time.ParseDuration(raw + "ms")
	if err != nil {
		return time.Now().UTC()
	}
	return time.Unix(0, ms.Nanoseconds()).UTC()
}

func writeLarkJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func logLarkWebhookWarn(logger *slog.Logger, msg string, args ...any) {
	if logger == nil {
		return
	}
	logger.Warn(msg, args...)
}

func logLarkWebhookDebug(logger *slog.Logger, msg string, args ...any) {
	if logger == nil {
		return
	}
	logger.Debug(msg, args...)
}

func (h *larkWebhookHeader) GetEventID() string {
	if h == nil {
		return ""
	}
	return strings.TrimSpace(h.EventID)
}
