package mixinapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/curve25519"
)

const (
	EncryptedMessageStateSuccess = "SUCCESS"
	EncryptedMessageStateFailed  = "FAILED"
	encryptedMessageVersion      = byte(1)
	encryptedSessionBytes        = 64
)

type Session struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	PublicKey string `json:"public_key"`
}

type recipientSession struct {
	SessionID string `json:"session_id"`
}

type encryptedMessageRequest struct {
	MessageRequest
	RecipientSessions []recipientSession `json:"recipient_sessions"`
	Checksum          string             `json:"checksum"`
}

type EncryptedMessageResponse struct {
	Type        string    `json:"type"`
	MessageID   string    `json:"message_id"`
	RecipientID string    `json:"recipient_id"`
	State       string    `json:"state"`
	Sessions    []Session `json:"sessions"`
}

type EncryptedMessageError struct {
	Responses []EncryptedMessageResponse
}

func (e *EncryptedMessageError) Error() string {
	ids := make([]string, 0, len(e.Responses))
	for _, response := range e.Responses {
		ids = append(ids, response.MessageID)
	}
	return fmt.Sprintf("encrypted mixin messages failed after refreshing sessions: %s", strings.Join(ids, ", "))
}

func PlainMessageCategory(category string) string {
	category = strings.ToUpper(strings.TrimSpace(category))
	if strings.HasPrefix(category, "ENCRYPTED_") {
		return "PLAIN_" + strings.TrimPrefix(category, "ENCRYPTED_")
	}
	return category
}

func encryptedMessageCategory(category string) string {
	category = strings.ToUpper(strings.TrimSpace(category))
	if strings.HasPrefix(category, "PLAIN_") {
		return "ENCRYPTED_" + strings.TrimPrefix(category, "PLAIN_")
	}
	return category
}

func isEncryptedMessageCategory(category string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(category)), "ENCRYPTED_")
}

func encryptMessageData(dataBase64 string, sessions []Session, privateKey ed25519.PrivateKey) (string, error) {
	plain, err := decodeMessageBase64(dataBase64)
	if err != nil {
		return "", fmt.Errorf("decode mixin message payload: %w", err)
	}
	if len(sessions) == 0 || len(sessions) > int(^uint16(0)) {
		return "", fmt.Errorf("mixin recipient sessions count is invalid")
	}
	curvePrivate, curvePublic, err := curveKeyPair(privateKey)
	if err != nil {
		return "", err
	}
	key := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", err
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, nil)

	result := make([]byte, 0, 35+len(sessions)*encryptedSessionBytes+len(nonce)+len(ciphertext))
	result = append(result, encryptedMessageVersion)
	var count [2]byte
	binary.LittleEndian.PutUint16(count[:], uint16(len(sessions)))
	result = append(result, count[:]...)
	result = append(result, curvePublic...)
	for _, session := range sessions {
		entry, err := encryptSessionKey(key, session, curvePrivate)
		if err != nil {
			return "", err
		}
		result = append(result, entry...)
	}
	result = append(result, nonce...)
	result = append(result, ciphertext...)
	return base64.RawURLEncoding.EncodeToString(result), nil
}

func encryptSessionKey(key []byte, session Session, privateKey []byte) ([]byte, error) {
	sessionID, err := uuid.Parse(strings.TrimSpace(session.SessionID))
	if err != nil || sessionID == uuid.Nil {
		return nil, fmt.Errorf("mixin session_id is invalid")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(session.PublicKey))
	if err != nil || len(publicKey) != curve25519.PointSize {
		return nil, fmt.Errorf("mixin session public_key is invalid")
	}
	shared, err := curve25519.X25519(privateKey, publicKey)
	if err != nil {
		return nil, fmt.Errorf("derive mixin session key: %w", err)
	}
	block, err := aes.NewCipher(shared)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	padded := make([]byte, aes.BlockSize*2)
	copy(padded, key)
	for index := len(key); index < len(padded); index++ {
		padded[index] = aes.BlockSize
	}
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(padded, padded)
	entry := make([]byte, 0, encryptedSessionBytes)
	entry = append(entry, sessionID[:]...)
	entry = append(entry, iv...)
	entry = append(entry, padded...)
	return entry, nil
}

func decryptMessageData(dataBase64, sessionID string, privateKey ed25519.PrivateKey) (string, error) {
	data, err := decodeMessageBase64(dataBase64)
	if err != nil {
		return "", fmt.Errorf("decode encrypted mixin message: %w", err)
	}
	if len(data) < 35+12+16 || data[0] != encryptedMessageVersion {
		return "", fmt.Errorf("encrypted mixin message format is invalid")
	}
	count := int(binary.LittleEndian.Uint16(data[1:3]))
	prefixBytes := 35 + count*encryptedSessionBytes
	if count == 0 || prefixBytes+12+16 > len(data) {
		return "", fmt.Errorf("encrypted mixin message sessions are invalid")
	}
	wantedSessionID, err := uuid.Parse(strings.TrimSpace(sessionID))
	if err != nil || wantedSessionID == uuid.Nil {
		return "", fmt.Errorf("mixin session_id is invalid")
	}
	curvePrivate, _, err := curveKeyPair(privateKey)
	if err != nil {
		return "", err
	}
	var key []byte
	for offset := 35; offset < prefixBytes; offset += encryptedSessionBytes {
		entrySessionID, err := uuid.FromBytes(data[offset : offset+16])
		if err != nil || entrySessionID != wantedSessionID {
			continue
		}
		key, err = decryptSessionKey(data[3:35], data[offset+16:offset+encryptedSessionBytes], curvePrivate)
		if err != nil {
			return "", err
		}
		break
	}
	if len(key) != 16 {
		return "", fmt.Errorf("encrypted mixin message does not contain the current session")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, data[prefixBytes:prefixBytes+12], data[prefixBytes+12:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt mixin message payload: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(plain), nil
}

func decryptSessionKey(senderPublicKey, encrypted []byte, privateKey []byte) ([]byte, error) {
	if len(senderPublicKey) != curve25519.PointSize || len(encrypted) != 48 {
		return nil, fmt.Errorf("encrypted mixin session key is invalid")
	}
	shared, err := curve25519.X25519(privateKey, senderPublicKey)
	if err != nil {
		return nil, fmt.Errorf("derive mixin session key: %w", err)
	}
	block, err := aes.NewCipher(shared)
	if err != nil {
		return nil, err
	}
	plain := append([]byte(nil), encrypted[aes.BlockSize:]...)
	cipher.NewCBCDecrypter(block, encrypted[:aes.BlockSize]).CryptBlocks(plain, plain)
	padding := int(plain[len(plain)-1])
	if padding != aes.BlockSize || len(plain) != aes.BlockSize*2 {
		return nil, fmt.Errorf("encrypted mixin session key padding is invalid")
	}
	for _, value := range plain[len(plain)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("encrypted mixin session key padding is invalid")
		}
	}
	return plain[:aes.BlockSize], nil
}

func curveKeyPair(privateKey ed25519.PrivateKey) ([]byte, []byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, nil, fmt.Errorf("mixin private key is invalid")
	}
	digest := sha512.Sum512(privateKey.Seed())
	digest[0] &= 248
	digest[31] &= 127
	digest[31] |= 64
	curvePrivate := append([]byte(nil), digest[:32]...)
	curvePublic, err := curve25519.X25519(curvePrivate, curve25519.Basepoint)
	if err != nil {
		return nil, nil, err
	}
	return curvePrivate, curvePublic, nil
}

func decodeMessageBase64(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(strings.TrimSpace(value)); err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("invalid base64")
}

func sessionChecksum(sessions []Session) string {
	hash := md5.New()
	for _, session := range sessions {
		_, _ = io.WriteString(hash, session.SessionID)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func failedEncryptedMessages(messages []MessageRequest, responses []EncryptedMessageResponse) ([]MessageRequest, []EncryptedMessageResponse, error) {
	byID := make(map[string]EncryptedMessageResponse, len(responses))
	for _, response := range responses {
		byID[response.MessageID] = response
	}
	var failed []MessageRequest
	var failures []EncryptedMessageResponse
	for _, message := range messages {
		response, found := byID[message.MessageID]
		if !found {
			return nil, nil, fmt.Errorf("encrypted mixin message response missing for %s", message.MessageID)
		}
		switch response.State {
		case EncryptedMessageStateSuccess:
		case EncryptedMessageStateFailed:
			failed = append(failed, message)
			failures = append(failures, response)
		default:
			return nil, nil, fmt.Errorf("encrypted mixin message %s returned state %q", message.MessageID, response.State)
		}
	}
	return failed, failures, nil
}

func uniqueRecipientIDs(messages []MessageRequest) ([]string, error) {
	seen := make(map[string]bool, len(messages))
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		recipientID, err := normalizeUUID("recipient_id", message.RecipientID)
		if err != nil {
			return nil, err
		}
		if !seen[recipientID] {
			seen[recipientID] = true
			ids = append(ids, recipientID)
		}
	}
	return ids, nil
}

func (c *Client) buildEncryptedMessageRequests(ctx context.Context, messages []MessageRequest) ([]encryptedMessageRequest, error) {
	recipientIDs, err := uniqueRecipientIDs(messages)
	if err != nil {
		return nil, err
	}
	sessionsByUser, err := c.recipientSessions(ctx, recipientIDs)
	if err != nil {
		return nil, err
	}
	requests := make([]encryptedMessageRequest, 0, len(messages))
	for _, message := range messages {
		message.RecipientID, _ = normalizeUUID("recipient_id", message.RecipientID)
		sessions := append([]Session(nil), sessionsByUser[message.RecipientID]...)
		sort.Slice(sessions, func(left, right int) bool { return sessions[left].SessionID < sessions[right].SessionID })
		data, err := encryptMessageData(message.DataBase64, sessions, c.credentials.privateKey)
		if err != nil {
			return nil, err
		}
		message.Category = encryptedMessageCategory(message.Category)
		request := encryptedMessageRequest{MessageRequest: message, Checksum: sessionChecksum(sessions)}
		for _, session := range sessions {
			request.RecipientSessions = append(request.RecipientSessions, recipientSession{SessionID: session.SessionID})
		}
		request.DataBase64 = data
		requests = append(requests, request)
	}
	return requests, nil
}

func (c *Client) recipientSessions(ctx context.Context, recipientIDs []string) (map[string][]Session, error) {
	found := make(map[string][]Session, len(recipientIDs))
	var missing []string
	c.sessionsMu.RLock()
	for _, recipientID := range recipientIDs {
		if sessions := c.sessions[recipientID]; len(sessions) > 0 {
			found[recipientID] = append([]Session(nil), sessions...)
		} else {
			missing = append(missing, recipientID)
		}
	}
	c.sessionsMu.RUnlock()
	if len(missing) == 0 {
		return found, nil
	}
	var fetched []Session
	if err := c.do(ctx, "POST", "/sessions/fetch", missing, &fetched, maxMessageRequestBytes); err != nil {
		return nil, fmt.Errorf("fetch mixin recipient sessions: %w", err)
	}
	for _, session := range fetched {
		userID := strings.TrimSpace(session.UserID)
		if userID == "" && len(missing) == 1 {
			userID = missing[0]
		}
		if _, err := normalizeUUID("session_id", session.SessionID); err != nil {
			return nil, err
		}
		publicKey, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(session.PublicKey))
		if err != nil || len(publicKey) != curve25519.PointSize {
			return nil, fmt.Errorf("mixin session public_key is invalid")
		}
		session.UserID = userID
		found[userID] = append(found[userID], session)
	}
	for _, recipientID := range missing {
		if len(found[recipientID]) == 0 {
			return nil, fmt.Errorf("no mixin sessions found for recipient %s", recipientID)
		}
	}
	c.sessionsMu.Lock()
	for _, recipientID := range missing {
		c.sessions[recipientID] = append([]Session(nil), found[recipientID]...)
	}
	c.sessionsMu.Unlock()
	return found, nil
}

func (c *Client) deleteRecipientSessions(messages []MessageRequest) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()
	for _, message := range messages {
		recipientID, err := normalizeUUID("recipient_id", message.RecipientID)
		if err == nil {
			delete(c.sessions, recipientID)
		}
	}
}
