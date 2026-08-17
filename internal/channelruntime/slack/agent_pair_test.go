package slack

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/agentpair"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestSlackPairTargetRequiresOneUserMention(t *testing.T) {
	target, err := slackPairTarget("<@U234>", "T123")
	if err != nil {
		t.Fatalf("slackPairTarget() error = %v", err)
	}
	if target.ID != "slack:T123:U234" || target.Contact.SlackUserID != "U234" {
		t.Fatalf("slackPairTarget() = %#v", target)
	}

	for _, raw := range []string{"", "@AgentB", "<@U234> extra", "<@C234>", "<@U234> <@U235>"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := slackPairTarget(raw, "T123"); err == nil {
				t.Fatalf("slackPairTarget(%q) expected error", raw)
			}
		})
	}
}

func TestSlackPairedAgentBypassesAllowlistOnlyInPrivateChat(t *testing.T) {
	allowedTeams := map[string]bool{"T100": true}
	allowedChannels := map[string]bool{"C100": true}
	tests := []struct {
		name        string
		teamID      string
		channelID   string
		chatType    string
		fromAgent   bool
		pairedAgent bool
		want        bool
	}{
		{name: "configured channel", teamID: "T100", channelID: "C100", chatType: "channel", want: true},
		{name: "unconfigured private human", teamID: "T100", channelID: "D200", chatType: "im"},
		{name: "paired private Agent", teamID: "T200", channelID: "D200", chatType: "im", fromAgent: true, pairedAgent: true, want: true},
		{name: "paired flag does not authorize human", teamID: "T200", channelID: "D200", chatType: "im", pairedAgent: true},
		{name: "paired Agent does not bypass group", teamID: "T200", channelID: "C200", chatType: "channel", fromAgent: true, pairedAgent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slackChatAuthorized(allowedTeams, allowedChannels, tt.teamID, tt.channelID, tt.chatType, tt.fromAgent, tt.pairedAgent)
			if got != tt.want {
				t.Fatalf("slackChatAuthorized() = %v, want %v", got, tt.want)
			}
		})
	}
	if !slackChatAuthorized(nil, nil, "T200", "D200", "im", false, false) {
		t.Fatal("empty allowlists should allow ordinary chats")
	}
}

func TestSlackPostDirectMessageOpensConversation(t *testing.T) {
	var openedUser string
	var posted struct {
		Channel string `json:"channel"`
		Text    string `json:"text"`
	}
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/conversations.open":
			var request struct {
				Users string `json:"users"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode conversations.open: %v", err)
			}
			openedUser = request.Users
			_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"D900"}}`))
		case "/chat.postMessage":
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode chat.postMessage: %v", err)
			}
			_, _ = w.Write([]byte(`{"ok":true,"channel":"D900","ts":"1.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	api := newSlackAPI(server.Client, server.URL, "bot-token", "app-token")
	if err := api.postDirectMessage(context.Background(), "U234", "pair-control"); err != nil {
		t.Fatalf("postDirectMessage() error = %v", err)
	}
	if openedUser != "U234" || posted.Channel != "D900" || posted.Text != "pair-control" {
		t.Fatalf("opened=%q posted=%#v", openedUser, posted)
	}
}

func TestSlackPrivateAdminPairCommandRunsBeforeAllowlist(t *testing.T) {
	var postedChannels []string
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/conversations.open":
			_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"D900"}}`))
		case "/chat.postMessage":
			var request struct {
				Channel string `json:"channel"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode chat.postMessage: %v", err)
			}
			postedChannels = append(postedChannels, request.Channel)
			_, _ = w.Write([]byte(`{"ok":true,"channel":"` + request.Channel + `","ts":"1.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	api := newSlackAPI(server.Client, server.URL, "bot-token", "app-token")
	root := t.TempDir()
	store := contacts.NewFileStore(filepath.Join(root, "contacts"))
	admins, err := agentpair.ParseAdmins([]string{"slack:T123:U11"})
	if err != nil {
		t.Fatalf("ParseAdmins() error = %v", err)
	}
	manager, err := agentpair.New(agentpair.Options{
		Self:       slackInboundAgentPeer("T123", "U100", "agent_a", "Agent A", ""),
		Admins:     admins,
		Contacts:   contacts.NewService(store),
		JournalDir: filepath.Join(root, "journal"),
		Send: func(ctx context.Context, target agentpair.Peer, body string) error {
			userID, targetErr := slackPairSendUserID(target)
			if targetErr != nil {
				return targetErr
			}
			return api.postDirectMessage(ctx, userID, body)
		},
	})
	if err != nil {
		t.Fatalf("agentpair.New() error = %v", err)
	}
	state := &slackRuntimeState{
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		api:             api,
		pairManager:     manager,
		botUserID:       "U100",
		botID:           "B100",
		allowedTeams:    map[string]bool{"T999": true},
		allowedChannels: map[string]bool{"C999": true},
	}
	payload, err := json.Marshal(map[string]any{
		"team_id":    "T123",
		"event_id":   "Ev1",
		"event_time": 1786932000,
		"event": map[string]any{
			"type":         "message",
			"user":         "U11",
			"text":         "/pair <@U234>",
			"channel":      "D11",
			"channel_type": "im",
			"ts":           "1.0",
		},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := state.handleSocketEnvelope(context.Background(), slackSocketEnvelope{Type: "events_api", Payload: payload}); err != nil {
		t.Fatalf("handleSocketEnvelope() error = %v", err)
	}
	if len(postedChannels) != 2 || postedChannels[0] != "D900" || postedChannels[1] != "D11" {
		t.Fatalf("posted channels = %v", postedChannels)
	}
	var eventTypes []string
	if err := domainjournal.ReplayDir(filepath.Join(root, "journal"), func(record domainjournal.Record) error {
		eventTypes = append(eventTypes, record.Event.Type)
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	if len(eventTypes) != 1 || eventTypes[0] != "agent_pair_requested" {
		t.Fatalf("journal events = %v", eventTypes)
	}
}
