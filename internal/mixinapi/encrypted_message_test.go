package mixinapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/curve25519"
)

func TestEncryptedMessageDataRoundTrip(t *testing.T) {
	sender := testCredentials()
	recipient := encryptedTestCredentials(0x44)
	session := encryptedTestSession(t, recipient)
	plain := base64.RawURLEncoding.EncodeToString([]byte("hello encrypted mixin"))

	encrypted, err := encryptMessageData(plain, []Session{session}, sender.privateKey)
	if err != nil {
		t.Fatalf("encryptMessageData() error = %v", err)
	}
	if encrypted == plain {
		t.Fatal("encrypted payload equals plaintext payload")
	}
	decrypted, err := decryptMessageData(encrypted, recipient.SessionID, recipient.privateKey)
	if err != nil {
		t.Fatalf("decryptMessageData() error = %v", err)
	}
	if decrypted != plain {
		t.Fatalf("decrypted = %q, want %q", decrypted, plain)
	}
}

func TestClientSendMessagesEncryptsAndCachesRecipientSessions(t *testing.T) {
	recipient := encryptedTestCredentials(0x55)
	session := encryptedTestSession(t, recipient)
	var fetchCalls atomic.Int64
	var sendCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sessions/fetch":
			fetchCalls.Add(1)
			var recipients []string
			if err := json.NewDecoder(r.Body).Decode(&recipients); err != nil {
				t.Error(err)
				return
			}
			if len(recipients) != 1 || recipients[0] != recipient.ClientID {
				t.Errorf("session recipients = %#v", recipients)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []Session{session}})
		case "/encrypted_messages":
			sendCalls.Add(1)
			var requests []encryptedMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&requests); err != nil {
				t.Error(err)
				return
			}
			if len(requests) != 1 {
				t.Errorf("encrypted requests = %d, want 1", len(requests))
				return
			}
			request := requests[0]
			if request.Category != MessageCategoryEncryptedText {
				t.Errorf("category = %q", request.Category)
			}
			if len(request.RecipientSessions) != 1 || request.RecipientSessions[0].SessionID != recipient.SessionID {
				t.Errorf("recipient sessions = %#v", request.RecipientSessions)
			}
			decrypted, err := decryptMessageData(request.DataBase64, recipient.SessionID, recipient.privateKey)
			if err != nil {
				t.Errorf("decrypt request: %v", err)
				return
			}
			if decoded, err := base64.RawURLEncoding.DecodeString(decrypted); err != nil || string(decoded) != "hello" {
				t.Errorf("decrypted request = %q, %v", decoded, err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []EncryptedMessageResponse{{
				MessageID: request.MessageID, RecipientID: request.RecipientID, State: EncryptedMessageStateSuccess,
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(testCredentials(), ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for _, messageID := range []string{"10000000-0000-4000-8000-000000000001", "10000000-0000-4000-8000-000000000002"} {
		err = client.SendMessages(context.Background(), []MessageRequest{{
			ConversationID: "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34",
			RecipientID:    recipient.ClientID,
			MessageID:      messageID,
			Category:       MessageCategoryPlainText,
			DataBase64:     base64.RawURLEncoding.EncodeToString([]byte("hello")),
		}})
		if err != nil {
			t.Fatalf("SendMessages() error = %v", err)
		}
	}
	if fetchCalls.Load() != 1 || sendCalls.Load() != 2 {
		t.Fatalf("fetch calls = %d, send calls = %d", fetchCalls.Load(), sendCalls.Load())
	}
}

func TestClientSendMessagesRefreshesRejectedSessions(t *testing.T) {
	oldRecipient := encryptedTestCredentials(0x61)
	newRecipient := encryptedTestCredentials(0x62)
	newRecipient.ClientID = oldRecipient.ClientID
	newRecipient.SessionID = "33333333-3333-4333-8333-333333333333"
	oldSession := encryptedTestSession(t, oldRecipient)
	newSession := encryptedTestSession(t, newRecipient)
	var fetchCalls atomic.Int64
	var sentSessionIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sessions/fetch":
			if fetchCalls.Add(1) == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []Session{oldSession}})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []Session{newSession}})
			}
		case "/encrypted_messages":
			var requests []encryptedMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&requests); err != nil {
				t.Error(err)
				return
			}
			request := requests[0]
			sentSessionIDs = append(sentSessionIDs, request.RecipientSessions[0].SessionID)
			state := EncryptedMessageStateFailed
			if len(sentSessionIDs) == 2 {
				state = EncryptedMessageStateSuccess
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []EncryptedMessageResponse{{
				MessageID: request.MessageID, RecipientID: request.RecipientID, State: state,
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(testCredentials(), ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = client.SendMessages(context.Background(), []MessageRequest{{
		ConversationID: "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34",
		RecipientID:    oldRecipient.ClientID,
		MessageID:      "10000000-0000-4000-8000-000000000003",
		Category:       MessageCategoryPlainText,
		DataBase64:     base64.RawURLEncoding.EncodeToString([]byte("refresh sessions")),
	}})
	if err != nil {
		t.Fatalf("SendMessages() error = %v", err)
	}
	if fetchCalls.Load() != 2 || len(sentSessionIDs) != 2 || sentSessionIDs[0] != oldRecipient.SessionID || sentSessionIDs[1] != newRecipient.SessionID {
		t.Fatalf("fetches = %d, sessions = %#v", fetchCalls.Load(), sentSessionIDs)
	}
}

func TestBlazeClientDecryptsEncryptedMessage(t *testing.T) {
	recipient := encryptedTestCredentials(0x66)
	sender := testCredentials()
	plain := base64.RawURLEncoding.EncodeToString([]byte("hello from blaze"))
	encrypted, err := encryptMessageData(plain, []Session{encryptedTestSession(t, recipient)}, sender.privateKey)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewBlazeClient(recipient, BlazeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	message := MessageView{Category: MessageCategoryEncryptedText, DataBase64: encrypted}
	if err := client.decryptMessage(&message); err != nil {
		t.Fatalf("decryptMessage() error = %v", err)
	}
	if message.DataBase64 != plain || message.Category != MessageCategoryEncryptedText {
		t.Fatalf("message = %#v", message)
	}
}

func TestBlazeClientDoesNotDecryptOwnEncryptedMessage(t *testing.T) {
	credentials := testCredentials()
	client, err := NewBlazeClient(credentials, BlazeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	message := MessageView{
		UserID: credentials.ClientID, Category: MessageCategoryEncryptedText, DataBase64: "not-for-this-session",
	}
	if err := client.decryptMessage(&message); err != nil {
		t.Fatalf("decryptMessage() error = %v", err)
	}
}

func encryptedTestCredentials(seedByte byte) Credentials {
	return Credentials{
		ClientID:   "11111111-1111-4111-8111-111111111111",
		SessionID:  "22222222-2222-4222-8222-222222222222",
		privateKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seedByte}, ed25519.SeedSize)),
	}
}

func encryptedTestSession(t *testing.T, credentials Credentials) Session {
	t.Helper()
	digest := sha512.Sum512(credentials.privateKey.Seed())
	digest[0] &= 248
	digest[31] &= 127
	digest[31] |= 64
	publicKey, err := curve25519.X25519(digest[:32], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return Session{
		UserID: credentials.ClientID, SessionID: credentials.SessionID,
		PublicKey: base64.RawURLEncoding.EncodeToString(publicKey),
	}
}
