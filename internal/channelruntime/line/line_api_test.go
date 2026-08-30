package line

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestLineAPIReplyMessage(t *testing.T) {
	t.Parallel()

	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/bot/message/reply" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/v2/bot/message/reply")
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer line-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer line-token")
		}
		if got := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.Contains(got, "application/json") {
			t.Fatalf("content-type = %q", got)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload lineReplyRequest
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.ReplyToken != "rtok_123" {
			t.Fatalf("reply_token = %q, want %q", payload.ReplyToken, "rtok_123")
		}
		if len(payload.Messages) != 1 || payload.Messages[0].Type != "text" || payload.Messages[0].Text != "hello line" {
			t.Fatalf("messages = %#v, want single text message", payload.Messages)
		}
		w.WriteHeader(http.StatusOK)
	}))

	api := newLineAPI(srv.Client, srv.URL, "line-token")
	if err := api.replyMessage(context.Background(), "rtok_123", "hello line"); err != nil {
		t.Fatalf("replyMessage() error = %v", err)
	}
}

func TestLineAPIPushMessage(t *testing.T) {
	t.Parallel()

	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/bot/message/push" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/v2/bot/message/push")
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer line-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer line-token")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload linePushRequest
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.To != "Cgroup123" {
			t.Fatalf("to = %q, want %q", payload.To, "Cgroup123")
		}
		if len(payload.Messages) != 1 || payload.Messages[0].Type != "text" || payload.Messages[0].Text != "hello line" {
			t.Fatalf("messages = %#v, want single text message", payload.Messages)
		}
		w.WriteHeader(http.StatusOK)
	}))

	api := newLineAPI(srv.Client, srv.URL, "line-token")
	if err := api.pushMessage(context.Background(), "Cgroup123", "hello line"); err != nil {
		t.Fatalf("pushMessage() error = %v", err)
	}
}

func TestLineAPIBotUserID(t *testing.T) {
	t.Parallel()

	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/bot/info" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/v2/bot/info")
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer line-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer line-token")
		}
		_, _ = w.Write([]byte(`{"userId":"Ubot001"}`))
	}))

	api := newLineAPI(srv.Client, srv.URL, "line-token")
	userID, err := api.botUserID(context.Background())
	if err != nil {
		t.Fatalf("botUserID() error = %v", err)
	}
	if userID != "Ubot001" {
		t.Fatalf("bot user id = %q, want %q", userID, "Ubot001")
	}
}

func TestLineAPIUserProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		chatType string
		chatID   string
		wantPath string
	}{
		{name: "private", chatType: "user", chatID: "Ualice", wantPath: "/v2/bot/profile/Ualice"},
		{name: "group", chatType: "group", chatID: "Cgroup", wantPath: "/v2/bot/group/Cgroup/member/Ualice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.wantPath {
					t.Fatalf("path = %q, want %q", r.URL.Path, tt.wantPath)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer line-token" {
					t.Fatalf("authorization = %q", got)
				}
				_, _ = w.Write([]byte(`{"displayName":"Alice","userId":"Ualice","pictureUrl":"https://cdn.example/alice.png"}`))
			}))

			profile, err := newLineAPI(server.Client, server.URL, "line-token").userProfile(context.Background(), tt.chatType, tt.chatID, "Ualice")
			if err != nil {
				t.Fatalf("userProfile() error = %v", err)
			}
			if profile.PictureURL != "https://cdn.example/alice.png" || profile.DisplayName != "Alice" {
				t.Fatalf("profile = %+v", profile)
			}
		})
	}
}

func TestLineAPIMessageContent(t *testing.T) {
	t.Parallel()

	payload := []byte{0x89, 0x50, 0x4e, 0x47}
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/bot/message/m_1001/content" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/v2/bot/message/m_1001/content")
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer line-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer line-token")
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))

	api := newLineAPI(srv.Client, srv.URL, "line-token")
	raw, mimeType, err := api.messageContent(context.Background(), "m_1001", 1024)
	if err != nil {
		t.Fatalf("messageContent() error = %v", err)
	}
	if mimeType != "image/png" {
		t.Fatalf("mime type = %q, want image/png", mimeType)
	}
	if string(raw) != string(payload) {
		t.Fatalf("raw = %v, want %v", raw, payload)
	}
}

func TestLineAPIContentBaseURL(t *testing.T) {
	t.Parallel()

	api := newLineAPI(nil, "https://api.line.me", "line-token")
	if got := api.contentBaseURL(); got != "https://api-data.line.me" {
		t.Fatalf("contentBaseURL() = %q, want %q", got, "https://api-data.line.me")
	}
	custom := newLineAPI(nil, "https://line-proxy.example.com", "line-token")
	if got := custom.contentBaseURL(); got != "https://line-proxy.example.com" {
		t.Fatalf("contentBaseURL() custom = %q, want %q", got, "https://line-proxy.example.com")
	}
}

func TestSendLineTextFallbackPolicy(t *testing.T) {
	t.Parallel()

	t.Run("reply success", func(t *testing.T) {
		replyCalls := 0
		pushCalls := 0
		srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v2/bot/message/reply":
				replyCalls++
				w.WriteHeader(http.StatusOK)
			case "/v2/bot/message/push":
				pushCalls++
				w.WriteHeader(http.StatusOK)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))

		api := newLineAPI(srv.Client, srv.URL, "line-token")
		err := sendLineText(context.Background(), api, nil, "Cgroup123", "hello line", "rtok_ok")
		if err != nil {
			t.Fatalf("sendLineText() error = %v", err)
		}
		if replyCalls != 1 {
			t.Fatalf("reply calls = %d, want 1", replyCalls)
		}
		if pushCalls != 0 {
			t.Fatalf("push calls = %d, want 0", pushCalls)
		}
	})

	t.Run("fallback to push on reply token error", func(t *testing.T) {
		replyCalls := 0
		pushCalls := 0
		srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v2/bot/message/reply":
				replyCalls++
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"Invalid reply token"}`))
			case "/v2/bot/message/push":
				pushCalls++
				w.WriteHeader(http.StatusOK)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))

		api := newLineAPI(srv.Client, srv.URL, "line-token")
		err := sendLineText(context.Background(), api, nil, "Cgroup123", "hello line", "rtok_expired")
		if err != nil {
			t.Fatalf("sendLineText() error = %v", err)
		}
		if replyCalls != 1 {
			t.Fatalf("reply calls = %d, want 1", replyCalls)
		}
		if pushCalls != 1 {
			t.Fatalf("push calls = %d, want 1", pushCalls)
		}
	})

	t.Run("do not fallback on non-token reply error", func(t *testing.T) {
		replyCalls := 0
		pushCalls := 0
		srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v2/bot/message/reply":
				replyCalls++
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"The property, 'messages', in the request body is invalid"}`))
			case "/v2/bot/message/push":
				pushCalls++
				w.WriteHeader(http.StatusOK)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))

		api := newLineAPI(srv.Client, srv.URL, "line-token")
		err := sendLineText(context.Background(), api, nil, "Cgroup123", "hello line", "rtok_bad_payload")
		if err == nil {
			t.Fatalf("sendLineText() expected error")
		}
		if !strings.Contains(err.Error(), "messages") {
			t.Fatalf("sendLineText() error = %v, want messages-related error", err)
		}
		if replyCalls != 1 {
			t.Fatalf("reply calls = %d, want 1", replyCalls)
		}
		if pushCalls != 0 {
			t.Fatalf("push calls = %d, want 0", pushCalls)
		}
	})

	t.Run("push directly when reply token missing", func(t *testing.T) {
		replyCalls := 0
		pushCalls := 0
		srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v2/bot/message/reply":
				replyCalls++
				w.WriteHeader(http.StatusOK)
			case "/v2/bot/message/push":
				pushCalls++
				w.WriteHeader(http.StatusOK)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))

		api := newLineAPI(srv.Client, srv.URL, "line-token")
		err := sendLineText(context.Background(), api, nil, "Cgroup123", "hello line", "")
		if err != nil {
			t.Fatalf("sendLineText() error = %v", err)
		}
		if replyCalls != 0 {
			t.Fatalf("reply calls = %d, want 0", replyCalls)
		}
		if pushCalls != 1 {
			t.Fatalf("push calls = %d, want 1", pushCalls)
		}
	})
}
