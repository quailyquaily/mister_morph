package daemonruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	"github.com/spf13/viper"
)

func TestContactsChatProfileRouteFetchesAndReturnsItems(t *testing.T) {
	stateDir := t.TempDir()

	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.info" {
			t.Fatalf("unexpected slack path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-test" {
			t.Fatalf("unexpected slack authorization: %q", got)
		}
		if got := r.URL.Query().Get("channel"); got != "C999" {
			t.Fatalf("unexpected slack channel: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"channel": map[string]any{
				"id":         "C999",
				"name":       "ops-room",
				"is_channel": true,
				"icon": map[string]any{
					"image_72": "https://example.test/ops-room.png",
				},
			},
		})
	}))
	defer slackServer.Close()

	if err := contacts.NewFileStore(stateDir+"/contacts").PutContact(context.Background(), contacts.Contact{
		ContactID:       "slack:T111:U222",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelSlack,
		SlackTeamID:     "T111",
		SlackChannelIDs: []string{"C999"},
	}); err != nil {
		t.Fatalf("seed active contact: %v", err)
	}

	settings := viper.New()
	settings.Set("file_state_dir", stateDir)
	settings.Set("slack.bot_token", "xoxb-test")
	settings.Set("slack.base_url", slackServer.URL)

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:                "serve",
		AuthToken:           "token",
		AgentSettingsReader: settings,
		RuntimePaths:        testRuntimePaths(stateDir),
	})

	req := httptest.NewRequest(http.MethodGet, "/contacts/chat-profile", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Exists    bool `json:"exists"`
		ItemCount int  `json:"item_count"`
		Items     []struct {
			ChatID   string `json:"chat_id"`
			Platform string `json:"platform"`
			Type     string `json:"type"`
			Name     string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !payload.Exists || payload.ItemCount != 1 || len(payload.Items) != 1 {
		t.Fatalf("unexpected payload shape: %#v", payload)
	}
	got := payload.Items[0]
	if got.ChatID != "slack:T111:C999" || got.Platform != "slack" || got.Type != "channel" || got.Name != "ops-room" {
		t.Fatalf("unexpected chat profile item: %#v", got)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid raw json: %v", err)
	}
	items, ok := raw["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("raw items mismatch: %#v", raw["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("raw item type = %T", items[0])
	}
	if _, ok := item["avatar_ref"]; ok {
		t.Fatalf("chat profile API should not include avatar_ref: %#v", item)
	}
}

func TestContactsChatProfileRouteReturnsNamelessCachedItems(t *testing.T) {
	stateDir := t.TempDir()

	if err := chatinfo.NewStore(stateDir+"/contacts").Write(context.Background(), []chatinfo.Info{
		{
			ChatID:    "lark:oc_123",
			Platform:  "lark",
			FetchedAt: time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, 7, 10, 1, 0, 0, 0, time.UTC),
		},
	}); err != nil {
		t.Fatalf("seed chat profile: %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "serve",
		AuthToken:    "token",
		RuntimePaths: testRuntimePaths(stateDir),
	})

	req := httptest.NewRequest(http.MethodGet, "/contacts/chat-profile", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		ItemCount int `json:"item_count"`
		Items     []struct {
			ChatID string `json:"chat_id"`
			Name   string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if payload.ItemCount != 1 || len(payload.Items) != 1 {
		t.Fatalf("unexpected payload shape: %#v", payload)
	}
	if got := payload.Items[0]; got.ChatID != "lark:oc_123" || got.Name != "" {
		t.Fatalf("unexpected item: %#v", got)
	}
}
