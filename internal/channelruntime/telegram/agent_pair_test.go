package telegram

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

func TestTelegramPairTargetRequiresOneUsername(t *testing.T) {
	target, err := telegramPairTarget("@AgentB")
	if err != nil {
		t.Fatalf("telegramPairTarget() error = %v", err)
	}
	if target.ID != "tg:@AgentB" || target.Contact.TGUsername != "AgentB" {
		t.Fatalf("telegramPairTarget() = %#v", target)
	}

	for _, raw := range []string{"", "AgentB", "@AgentB extra", "@Agent:B", "@@AgentB"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := telegramPairTarget(raw); err == nil {
				t.Fatalf("telegramPairTarget(%q) expected error", raw)
			}
		})
	}
}

func TestTelegramPairedAgentBypassesAllowlistOnlyInPrivateChat(t *testing.T) {
	allowed := map[int64]bool{100: true}
	tests := []struct {
		name        string
		chatID      int64
		isGroup     bool
		fromAgent   bool
		pairedAgent bool
		want        bool
	}{
		{name: "configured chat", chatID: 100, isGroup: true, want: true},
		{name: "unconfigured private human", chatID: 200},
		{name: "paired private Agent", chatID: 200, fromAgent: true, pairedAgent: true, want: true},
		{name: "paired flag does not authorize human", chatID: 200, pairedAgent: true},
		{name: "paired Agent does not bypass group allowlist", chatID: -200, isGroup: true, fromAgent: true, pairedAgent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := telegramChatAuthorized(allowed, tt.chatID, tt.isGroup, tt.fromAgent, tt.pairedAgent); got != tt.want {
				t.Fatalf("telegramChatAuthorized() = %v, want %v", got, tt.want)
			}
		})
	}
	if !telegramChatAuthorized(nil, 200, false, false, false) {
		t.Fatal("empty allowlist should allow ordinary chats")
	}
}

func TestTelegramSendDirectTextSupportsUsernameTarget(t *testing.T) {
	var request telegramDirectMessageRequest
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	api := newTelegramAPI(server.Client, server.URL, "token")
	if err := api.sendDirectText(context.Background(), "@AgentB", "pair-control"); err != nil {
		t.Fatalf("sendDirectText() error = %v", err)
	}
	if request.ChatID != "@AgentB" || request.Text != "pair-control" {
		t.Fatalf("request = %#v", request)
	}
}

func TestTelegramPrivateAdminPairCommandRunsBeforeAllowlist(t *testing.T) {
	var chatTargets []any
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		chatTargets = append(chatTargets, request["chat_id"])
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	api := newTelegramAPI(server.Client, server.URL, "token")
	root := t.TempDir()
	store := contacts.NewFileStore(filepath.Join(root, "contacts"))
	admins, err := agentpair.ParseAdmins([]string{"tg:@admin"})
	if err != nil {
		t.Fatalf("ParseAdmins() error = %v", err)
	}
	if err := store.PutContact(context.Background(), contacts.Contact{
		ContactID:       "tg:@admin",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		ContactNickname: "Admin",
		TGUsername:      "admin",
		TGPrivateChatID: 11,
	}); err != nil {
		t.Fatalf("PutContact() error = %v", err)
	}
	manager, err := agentpair.New(agentpair.Options{
		Self:       telegramInboundAgentPeer(1001, "agent_a", "Agent A", 0),
		Admins:     admins,
		Contacts:   contacts.NewService(store),
		JournalDir: filepath.Join(root, "journal"),
		Send: func(ctx context.Context, target agentpair.Peer, body string) error {
			chatTarget, targetErr := telegramPairSendTarget(target)
			if targetErr != nil {
				return targetErr
			}
			return api.sendDirectText(ctx, chatTarget, body)
		},
	})
	if err != nil {
		t.Fatalf("agentpair.New() error = %v", err)
	}
	state := &telegramRuntimeState{
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		api:              api,
		pairManager:      manager,
		allowedChatIDs:   map[int64]bool{99: true},
		botUser:          "agent_a",
		botID:            1001,
		groupTriggerMode: "strict",
	}
	state.handleUpdate(telegramUpdate{Message: &telegramMessage{
		MessageID: 1,
		Chat:      &telegramChat{ID: 11, Type: "private"},
		From:      &telegramUser{ID: 11, Username: "admin"},
		Text:      "/pair @AgentB",
	}})

	if len(chatTargets) != 2 || chatTargets[0] != "@AgentB" || chatTargets[1] != float64(11) {
		t.Fatalf("chat targets = %#v", chatTargets)
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

func TestTelegramUnpairedPrivateAgentDoesNotReceiveUnauthorizedReply(t *testing.T) {
	requestCount := 0
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	state := &telegramRuntimeState{
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		api:              newTelegramAPI(server.Client, server.URL, "token"),
		allowedChatIDs:   map[int64]bool{99: true},
		botUser:          "agent_a",
		botID:            1001,
		groupTriggerMode: "strict",
	}

	state.handleUpdate(telegramUpdate{Message: &telegramMessage{
		MessageID: 1,
		Chat:      &telegramChat{ID: 2002, Type: "private"},
		From:      &telegramUser{ID: 2002, IsBot: true, Username: "agent_b"},
		Text:      "hello",
	}})

	if requestCount != 0 {
		t.Fatalf("Telegram API request count = %d, want 0", requestCount)
	}
}
