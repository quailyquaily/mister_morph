package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
)

func TestAgentSendToolUsesContactsSendParameters(t *testing.T) {
	contactsTool := NewContactsSendTool(ContactsSendToolOptions{Enabled: true})
	agentTool := NewAgentSendTool(ContactsSendToolOptions{Enabled: true})

	if agentTool.ParameterSchema() != contactsTool.ParameterSchema() {
		t.Fatal("agent_send and contacts_send parameter schemas differ")
	}
}

func TestAgentSendToolOnlySendsToActiveAgents(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	contactsDir := filepath.Join(t.TempDir(), "contacts")
	svc := contacts.NewService(contacts.NewFileStore(contactsDir))
	for _, contact := range []contacts.Contact{
		{
			ContactID:      "contact:smith",
			Kind:           contacts.KindAgent,
			Channel:        contacts.ChannelTelegram,
			TGUsername:     "smith_bot",
			TGGroupChatIDs: []int64{-1001},
		},
		{
			ContactID:      "tg:@alice",
			Kind:           contacts.KindHuman,
			Channel:        contacts.ChannelTelegram,
			TGUsername:     "alice",
			TGGroupChatIDs: []int64{-1001},
		},
	} {
		if _, err := svc.UpsertContact(ctx, contact, now); err != nil {
			t.Fatalf("UpsertContact(%q) error = %v", contact.ContactID, err)
		}
	}

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 1},
		})
	}))
	defer server.Close()

	tool := NewAgentSendTool(ContactsSendToolOptions{
		Enabled:          true,
		ContactsDir:      contactsDir,
		TelegramBotToken: "token",
		TelegramBaseURL:  server.URL,
	})

	_, err := tool.Execute(ctx, map[string]any{
		"contact_id":   "tg:@smith_bot,tg:@alice",
		"message_text": "continue",
	})
	if err == nil || !strings.Contains(err.Error(), "not an active Agent") {
		t.Fatalf("mixed recipient Execute() error = %v, want active Agent rejection", err)
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("mixed recipient request count = %d, want 0", got)
	}
	_, err = tool.Execute(ctx, map[string]any{
		"contact_id":   "tg:-1001",
		"message_text": "continue",
	})
	if err == nil || !strings.Contains(err.Error(), "not an active Agent") {
		t.Fatalf("raw chat target Execute() error = %v, want active Agent rejection", err)
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("raw chat target request count = %d, want 0", got)
	}

	if _, err := tool.Execute(ctx, map[string]any{
		"contact_id":   "tg:@smith_bot",
		"message_text": "continue",
	}); err != nil {
		t.Fatalf("active Agent Execute() error = %v", err)
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("active Agent request count = %d, want 1", got)
	}
}
