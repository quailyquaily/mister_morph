package mixinapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientMeSignsEveryRequest(t *testing.T) {
	var tokens []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/me" {
			t.Errorf("request = %s %s", r.Method, r.URL.RequestURI())
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			t.Error("missing bearer token")
		}
		mu.Lock()
		tokens = append(tokens, token)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"user_id":"773e5e77-4107-45c2-b648-8fc722ed77f5","identity_number":"7000123456","full_name":"Morph","avatar_url":"https://example.test/avatar.png","app_id":"773e5e77-4107-45c2-b648-8fc722ed77f5"}}`)
	}))
	defer server.Close()

	var requestIDCounter atomic.Int64
	client, err := NewClient(testCredentials(), ClientOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Now:        func() time.Time { return time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC) },
		NewRequestID: func() string {
			if requestIDCounter.Add(1) == 1 {
				return "5f02a273-cd18-4af3-a57b-f3224a3c3591"
			}
			return "a4ec1e53-f147-439a-82cd-2e5e4a95a152"
		},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	for range 2 {
		user, getErr := client.Me(context.Background())
		if getErr != nil {
			t.Fatalf("Me() error = %v", getErr)
		}
		if user.IdentityNumber != "7000123456" || user.FullName != "Morph" {
			t.Fatalf("user = %#v", user)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(tokens) != 2 || tokens[0] == tokens[1] {
		t.Fatalf("tokens = %#v", tokens)
	}
}

func TestClientSendMessagesSignsExactBody(t *testing.T) {
	credentials := testCredentials()
	recipient := encryptedTestCredentials(0x71)
	session := encryptedTestSession(t, recipient)
	now := time.Date(2026, 8, 27, 3, 4, 5, 0, time.UTC)
	requestID := "5f02a273-cd18-4af3-a57b-f3224a3c3591"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/sessions/fetch" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []Session{session}})
			return
		}
		if r.URL.Path != "/encrypted_messages" {
			t.Errorf("path = %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		wantToken, err := signAuthenticationToken(credentials, http.MethodPost, "/encrypted_messages", body, now, requestID)
		if err != nil {
			t.Error(err)
			return
		}
		if got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); got != wantToken {
			t.Errorf("token does not bind exact body")
		}
		_, _ = io.WriteString(w, `{"data":[{"message_id":"a4ec1e53-f147-439a-82cd-2e5e4a95a152","recipient_id":"11111111-1111-4111-8111-111111111111","state":"SUCCESS"}]}`)
	}))
	defer server.Close()
	client, err := NewClient(credentials, ClientOptions{
		BaseURL:      server.URL,
		HTTPClient:   server.Client(),
		Now:          func() time.Time { return now },
		NewRequestID: func() string { return requestID },
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.SendMessages(context.Background(), []MessageRequest{{
		ConversationID: "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34",
		RecipientID:    recipient.ClientID,
		MessageID:      "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
		Category:       MessageCategoryEncryptedText,
		DataBase64:     "SGVsbG8",
	}})
	if err != nil {
		t.Fatalf("SendMessages() error = %v", err)
	}
}

func TestClientSendMessagesRetriesWithStableBody(t *testing.T) {
	recipient := encryptedTestCredentials(0x72)
	session := encryptedTestSession(t, recipient)
	var (
		attempts int
		bodies   [][]byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/sessions/fetch" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []Session{session}})
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		attempts++
		bodies = append(bodies, body)
		if attempts == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"status":429,"code":429,"description":"rate limited"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"message_id":"a4ec1e53-f147-439a-82cd-2e5e4a95a152","recipient_id":"11111111-1111-4111-8111-111111111111","state":"SUCCESS"}]}`)
	}))
	defer server.Close()
	client, err := NewClient(testCredentials(), ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = client.SendMessages(context.Background(), []MessageRequest{{
		ConversationID: "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34",
		RecipientID:    recipient.ClientID,
		MessageID:      "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
		Category:       MessageCategoryEncryptedText,
		DataBase64:     "SGVsbG8",
	}})
	if err != nil {
		t.Fatalf("SendMessages() error = %v", err)
	}
	if attempts != 2 || !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatalf("attempts=%d stable_body=%v", attempts, len(bodies) == 2 && bytes.Equal(bodies[0], bodies[1]))
	}
}

func TestClientRejectsOversizedMessageBatchBeforeRequest(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called.Store(true)
	}))
	defer server.Close()
	client, err := NewClient(testCredentials(), ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = client.SendMessages(context.Background(), []MessageRequest{{
		ConversationID: "8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34",
		RecipientID:    "11111111-1111-4111-8111-111111111111",
		MessageID:      "a4ec1e53-f147-439a-82cd-2e5e4a95a152",
		Category:       MessageCategoryEncryptedText,
		DataBase64:     strings.Repeat("a", maxMessageRequestBytes),
	}})
	if !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("error = %v", err)
	}
	if called.Load() {
		t.Fatal("server should not be called")
	}
}

func TestClientReturnsStructuredAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"status":429,"code":429,"description":"rate limited"}}`)
	}))
	defer server.Close()
	client, err := NewClient(testCredentials(), ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Me(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if apiErr.HTTPStatus != http.StatusTooManyRequests || apiErr.Code != 429 || apiErr.RetryAfter != 7*time.Second {
		t.Fatalf("api error = %#v", apiErr)
	}
}

func TestDecodeResponseRejectsTrailingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{}} {"data":{}}`)
	}))
	defer server.Close()
	client, err := NewClient(testCredentials(), ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Me(context.Background()); err == nil {
		t.Fatal("Me() expected trailing JSON error")
	}
}

func testCredentials() Credentials {
	return Credentials{
		ClientID:   "773e5e77-4107-45c2-b648-8fc722ed77f5",
		SessionID:  "a34c07a9-755d-4b54-94c5-e45e9a2dd43e",
		privateKey: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x33}, ed25519.SeedSize)),
	}
}

func decodeRequestBody(t *testing.T, r *http.Request, target any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}
