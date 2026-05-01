package lark

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseLarkWebhookRequestURLVerification(t *testing.T) {
	t.Parallel()

	body := []byte(`{"type":"url_verification","challenge":"challenge_123","token":"verify_tok"}`)
	plain, reqType, eventType, token, challenge, err := parseLarkWebhookRequest(body, http.Header{}, "")
	if err != nil {
		t.Fatalf("parseLarkWebhookRequest() error = %v", err)
	}
	if string(plain) != string(body) {
		t.Fatalf("plain body mismatch: got %q want %q", string(plain), string(body))
	}
	if reqType != "url_verification" {
		t.Fatalf("req_type mismatch: got %q want %q", reqType, "url_verification")
	}
	if eventType != "" {
		t.Fatalf("event_type mismatch: got %q want empty", eventType)
	}
	if token != "verify_tok" {
		t.Fatalf("token mismatch: got %q want %q", token, "verify_tok")
	}
	if challenge != "challenge_123" {
		t.Fatalf("challenge mismatch: got %q want %q", challenge, "challenge_123")
	}
}

func TestVerifyLarkWebhookSignature(t *testing.T) {
	t.Parallel()

	body := []byte(`{"encrypt":"payload"}`)
	headers := http.Header{}
	headers.Set(larkRequestTimestampHeader, "1710000000")
	headers.Set(larkRequestNonceHeader, "nonce_123")
	sum := sha256.Sum256([]byte("1710000000" + "nonce_123" + "encrypt_key_123" + string(body)))
	headers.Set(larkRequestSignatureHeader, hex.EncodeToString(sum[:]))

	if err := verifyLarkWebhookSignature(headers, "encrypt_key_123", body); err != nil {
		t.Fatalf("verifyLarkWebhookSignature() error = %v", err)
	}
}

func TestDecryptLarkWebhook(t *testing.T) {
	t.Parallel()

	plain := []byte(`{"type":"event_callback","header":{"event_id":"ev_001"}}`)
	encryptKey := "encrypt_key_123"
	encrypted := encryptLarkWebhookTestPayload(t, plain, encryptKey)

	decrypted, err := decryptLarkWebhook(encrypted, encryptKey)
	if err != nil {
		t.Fatalf("decryptLarkWebhook() error = %v", err)
	}
	if string(decrypted) != string(plain) {
		t.Fatalf("decrypted payload mismatch: got %q want %q", string(decrypted), string(plain))
	}
}

func TestInboundMessageFromWebhookEvent(t *testing.T) {
	t.Parallel()

	payload := larkWebhookEnvelope{
		Header: &larkWebhookHeader{
			EventID:   "ev_001",
			EventType: "im.message.receive_v1",
			Token:     "verify_tok",
		},
		Event: &larkWebhookEvent{
			Sender: larkWebhookSender{
				SenderType: "user",
				SenderID: larkWebhookUserID{
					OpenID: "ou_123",
				},
			},
			Message: larkWebhookMessage{
				MessageID:   "om_1001",
				CreateTime:  "1760000000123",
				ChatID:      "oc_group123",
				ChatType:    "group",
				MessageType: "text",
				Content:     `{"text":"hello lark"}`,
				Mentions: []larkWebhookMentionEvent{
					{ID: larkWebhookUserID{OpenID: "ou_123"}},
					{ID: larkWebhookUserID{OpenID: "ou_456"}},
					{ID: larkWebhookUserID{OpenID: "ou_123"}},
				},
			},
		},
	}

	msg, ok, err := inboundMessageFromWebhookEvent(payload, map[string]bool{})
	if err != nil {
		t.Fatalf("inboundMessageFromWebhookEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("inboundMessageFromWebhookEvent() ok=false, want true")
	}
	if msg.ChatID != "oc_group123" {
		t.Fatalf("chat_id mismatch: got %q want %q", msg.ChatID, "oc_group123")
	}
	if msg.ChatType != "group" {
		t.Fatalf("chat_type mismatch: got %q want %q", msg.ChatType, "group")
	}
	if msg.FromUserID != "ou_123" {
		t.Fatalf("from_user_id mismatch: got %q want %q", msg.FromUserID, "ou_123")
	}
	if msg.MessageID != "om_1001" {
		t.Fatalf("message_id mismatch: got %q want %q", msg.MessageID, "om_1001")
	}
	if msg.Text != "hello lark" {
		t.Fatalf("text mismatch: got %q want %q", msg.Text, "hello lark")
	}
	if msg.EventID != "ev_001" {
		t.Fatalf("event_id mismatch: got %q want %q", msg.EventID, "ev_001")
	}
	if len(msg.MentionUsers) != 2 {
		t.Fatalf("mention_users len = %d, want 2", len(msg.MentionUsers))
	}
	wantSentAt := time.UnixMilli(1760000000123).UTC()
	if !msg.SentAt.Equal(wantSentAt) {
		t.Fatalf("sent_at = %s, want %s", msg.SentAt.Format(time.RFC3339Nano), wantSentAt.Format(time.RFC3339Nano))
	}
}

func TestInboundMessageFromWebhookEventImage(t *testing.T) {
	t.Parallel()

	payload := larkWebhookEnvelope{
		Header: &larkWebhookHeader{
			EventID:   "ev_img",
			EventType: "im.message.receive_v1",
		},
		Event: &larkWebhookEvent{
			Sender: larkWebhookSender{
				SenderType: "user",
				SenderID:   larkWebhookUserID{OpenID: "ou_123"},
			},
			Message: larkWebhookMessage{
				MessageID:   "om_1001",
				CreateTime:  "1760000000123",
				ChatID:      "oc_group123",
				ChatType:    "group",
				MessageType: "image",
				Content:     `{"image_key":"img_123"}`,
			},
		},
	}

	msg, ok, err := inboundMessageFromWebhookEvent(payload, map[string]bool{})
	if err != nil {
		t.Fatalf("inboundMessageFromWebhookEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("inboundMessageFromWebhookEvent() ok=false, want true")
	}
	if msg.Text != "User sent an image." {
		t.Fatalf("text = %q, want image fallback", msg.Text)
	}
	if len(msg.ImageKeys) != 1 || msg.ImageKeys[0] != "img_123" {
		t.Fatalf("image keys = %#v, want [img_123]", msg.ImageKeys)
	}
}

func TestInboundMessageFromWebhookEventAllowlist(t *testing.T) {
	t.Parallel()

	payload := larkWebhookEnvelope{
		Event: &larkWebhookEvent{
			Sender: larkWebhookSender{
				SenderType: "user",
				SenderID:   larkWebhookUserID{OpenID: "ou_123"},
			},
			Message: larkWebhookMessage{
				MessageID:   "om_1001",
				ChatID:      "oc_denied",
				ChatType:    "group",
				MessageType: "text",
				Content:     `{"text":"hello"}`,
			},
		},
	}

	_, ok, err := inboundMessageFromWebhookEvent(payload, map[string]bool{"oc_allowed": true})
	if err != nil {
		t.Fatalf("inboundMessageFromWebhookEvent() error = %v", err)
	}
	if ok {
		t.Fatalf("inboundMessageFromWebhookEvent() ok=true, want false")
	}
}

func encryptLarkWebhookTestPayload(t *testing.T, plain []byte, encryptKey string) string {
	t.Helper()

	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatalf("aes.NewCipher() error = %v", err)
	}
	iv := []byte("0123456789abcdef")
	payload := append([]byte("prefixprefixprefix"), plain...)
	padding := aes.BlockSize - (len(payload) % aes.BlockSize)
	payload = append(payload, bytesRepeat(byte(padding), padding)...)
	ciphertext := make([]byte, len(payload))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, payload)
	out := append(append([]byte{}, iv...), ciphertext...)
	return base64.StdEncoding.EncodeToString(out)
}

func bytesRepeat(b byte, count int) []byte {
	if count <= 0 {
		return nil
	}
	out := make([]byte, count)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestParseLarkWebhookRequestEncryptedEvent(t *testing.T) {
	t.Parallel()

	plainBody := []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1","token":"verify_tok"},"event":{"sender":{"sender_type":"user","sender_id":{"open_id":"ou_123"}},"message":{"message_id":"om_1001","chat_id":"oc_group123","chat_type":"group","message_type":"text","content":"{\"text\":\"hello\"}"}}}`)
	encryptKey := "encrypt_key_123"
	encrypted := encryptLarkWebhookTestPayload(t, plainBody, encryptKey)
	body := []byte(`{"encrypt":"` + encrypted + `"}`)
	headers := http.Header{}
	headers.Set(larkRequestTimestampHeader, "1710000000")
	headers.Set(larkRequestNonceHeader, "nonce_123")
	sum := sha256.Sum256([]byte("1710000000" + "nonce_123" + encryptKey + string(body)))
	headers.Set(larkRequestSignatureHeader, hex.EncodeToString(sum[:]))

	plain, reqType, eventType, token, challenge, err := parseLarkWebhookRequest(body, headers, encryptKey)
	if err != nil {
		t.Fatalf("parseLarkWebhookRequest() error = %v", err)
	}
	if strings.TrimSpace(string(plain)) != string(plainBody) {
		t.Fatalf("plain body mismatch: got %q want %q", strings.TrimSpace(string(plain)), string(plainBody))
	}
	if reqType != "" {
		t.Fatalf("req_type mismatch: got %q want empty", reqType)
	}
	if eventType != "im.message.receive_v1" {
		t.Fatalf("event_type mismatch: got %q want %q", eventType, "im.message.receive_v1")
	}
	if token != "verify_tok" {
		t.Fatalf("token mismatch: got %q want %q", token, "verify_tok")
	}
	if challenge != "" {
		t.Fatalf("challenge mismatch: got %q want empty", challenge)
	}
}

func TestParseLarkWebhookRequestRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	plainBody := []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1","token":"verify_tok"}}`)
	body := []byte(`{"encrypt":"` + encryptLarkWebhookTestPayload(t, plainBody, "encrypt_key_123") + `"}`)
	headers := http.Header{}
	headers.Set(larkRequestTimestampHeader, "1710000000")
	headers.Set(larkRequestNonceHeader, "nonce_123")
	headers.Set(larkRequestSignatureHeader, "bad_signature")

	_, _, _, _, _, err := parseLarkWebhookRequest(body, headers, "encrypt_key_123")
	if err == nil {
		t.Fatalf("parseLarkWebhookRequest() expected error")
	}
}

func TestWebhookHandlerRequiresPost(t *testing.T) {
	t.Parallel()

	handler := newLarkWebhookHandler(larkWebhookHandlerOptions{})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/lark/webhook", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}
