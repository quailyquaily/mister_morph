package chatinfo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestFetcherOptionsFromReaderCapturesChannelCredentials(t *testing.T) {
	reader := viper.New()
	reader.Set("telegram.bot_token", " telegram-token ")
	reader.Set("slack.bot_token", " slack-token ")
	reader.Set("slack.base_url", " https://slack.example.test/api ")
	reader.Set("line.channel_access_token", " line-token ")
	reader.Set("line.base_url", " https://line.example.test ")
	reader.Set("lark.app_id", " lark-id ")
	reader.Set("lark.app_secret", " lark-secret ")
	reader.Set("lark.base_url", " https://lark.example.test/open-apis ")

	opts := FetcherOptionsFromReader(reader)
	if opts.TelegramBotToken != "telegram-token" || opts.SlackBotToken != "slack-token" || opts.LineChannelToken != "line-token" {
		t.Fatalf("channel tokens = %#v", opts)
	}
	if opts.SlackBaseURL != "https://slack.example.test/api" || opts.LineBaseURL != "https://line.example.test" {
		t.Fatalf("channel base URLs = %#v", opts)
	}
	if opts.LarkAppID != "lark-id" || opts.LarkAppSecret != "lark-secret" || opts.LarkBaseURL != "https://lark.example.test/open-apis" {
		t.Fatalf("lark options = %#v", opts)
	}
}

func TestFetcherRefreshTelegramGetChat(t *testing.T) {
	var gotPath, gotChatID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotChatID = r.URL.Query().Get("chat_id")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"id":    -100123.0,
				"type":  "supergroup",
				"title": "Project Room",
				"photo": map[string]any{
					"small_file_id": "small-file",
				},
			},
		})
	}))
	defer server.Close()

	fetcher := NewFetcher(FetcherOptions{
		TelegramBotToken: "token",
		TelegramBaseURL:  server.URL,
	})
	info, err := fetcher.RefreshChatInfo(context.Background(), "tg:-100123_77")
	if err != nil {
		t.Fatalf("RefreshChatInfo() error = %v", err)
	}
	if gotPath != "/bottoken/getChat" || gotChatID != "-100123" {
		t.Fatalf("telegram request mismatch: path=%q chat_id=%q", gotPath, gotChatID)
	}
	if info.ChatID != "tg:-100123_77" || info.Platform != "telegram" || info.Type != "supergroup" || info.Name != "Project Room" {
		t.Fatalf("info mismatch: %#v", info)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	if strings.Contains(string(encoded), "avatar_ref") {
		t.Fatalf("chat profile should not expose avatar_ref: %s", encoded)
	}
}

func TestFetcherRefreshSlackConversationInfo(t *testing.T) {
	var gotAuth, gotChannel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotChannel = r.URL.Query().Get("channel")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"channel": map[string]any{
				"id":         "C999",
				"name":       "release-room",
				"is_channel": true,
				"icon": map[string]any{
					"image_72": "https://example.test/icon.png",
				},
			},
		})
	}))
	defer server.Close()

	fetcher := NewFetcher(FetcherOptions{
		SlackBotToken: "xoxb-token",
		SlackBaseURL:  server.URL,
	})
	info, err := fetcher.RefreshChatInfo(context.Background(), "slack:T111:C999")
	if err != nil {
		t.Fatalf("RefreshChatInfo() error = %v", err)
	}
	if gotAuth != "Bearer xoxb-token" || gotChannel != "C999" {
		t.Fatalf("slack request mismatch: auth=%q channel=%q", gotAuth, gotChannel)
	}
	if info.ChatID != "slack:T111:C999" || info.Platform != "slack" || info.Type != "channel" || info.Name != "release-room" {
		t.Fatalf("info mismatch: %#v", info)
	}
}

func TestFetcherRefreshLineGroupSummary(t *testing.T) {
	var gotAuth, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"groupId":    "Cabcdef",
			"groupName":  "LINE Group",
			"pictureUrl": "https://example.test/line.png",
		})
	}))
	defer server.Close()

	fetcher := NewFetcher(FetcherOptions{
		LineChannelToken: "line-token",
		LineBaseURL:      server.URL,
	})
	info, err := fetcher.RefreshChatInfo(context.Background(), "line:Cabcdef")
	if err != nil {
		t.Fatalf("RefreshChatInfo() error = %v", err)
	}
	if gotAuth != "Bearer line-token" || gotPath != "/v2/bot/group/Cabcdef/summary" {
		t.Fatalf("line request mismatch: auth=%q path=%q", gotAuth, gotPath)
	}
	if info.ChatID != "line:Cabcdef" || info.Platform != "line" || info.Type != "group" || info.Name != "LINE Group" {
		t.Fatalf("info mismatch: %#v", info)
	}
}

func TestFetcherRefreshLarkChat(t *testing.T) {
	var sawTokenRequest bool
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v3/tenant_access_token/internal":
			sawTokenRequest = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			})
		case "/im/v1/chats/oc_abc":
			gotAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"chat_id":   "oc_abc",
					"name":      "Lark Chat",
					"chat_type": "group",
					"avatar":    "https://example.test/lark.png",
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	fetcher := NewFetcher(FetcherOptions{
		LarkAppID:     "app-id",
		LarkAppSecret: "secret",
		LarkBaseURL:   server.URL,
	})
	info, err := fetcher.RefreshChatInfo(context.Background(), "lark:oc_abc")
	if err != nil {
		t.Fatalf("RefreshChatInfo() error = %v", err)
	}
	if !sawTokenRequest || gotAuth != "Bearer tenant-token" {
		t.Fatalf("lark auth mismatch: sawToken=%v auth=%q", sawTokenRequest, gotAuth)
	}
	if info.ChatID != "lark:oc_abc" || info.Platform != "lark" || info.Type != "group" || info.Name != "Lark Chat" {
		t.Fatalf("info mismatch: %#v", info)
	}
}

func TestFetcherRejectsUnsupportedLineRoomSummary(t *testing.T) {
	fetcher := NewFetcher(FetcherOptions{LineChannelToken: "token"})
	_, err := fetcher.RefreshChatInfo(context.Background(), "line:Rroom")
	if err == nil {
		t.Fatalf("RefreshChatInfo() expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "room") {
		t.Fatalf("error = %q, want room mention", err.Error())
	}
}
