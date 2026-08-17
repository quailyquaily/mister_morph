package contacts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPairAgentIsOnlyStructuredPathThatCanGrantPaired(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	service := NewService(store)
	now := time.Date(2026, 8, 17, 7, 0, 0, 0, time.UTC)

	ordinary, err := service.UpsertContact(ctx, Contact{
		ContactID:       "tg:@peer_bot",
		Kind:            KindAgent,
		Channel:         ChannelTelegram,
		ContactNickname: "Peer",
		TGUsername:      "peer_bot",
		Paired:          true,
	}, now)
	if err != nil {
		t.Fatalf("UpsertContact() error = %v", err)
	}
	if ordinary.Paired {
		t.Fatal("ordinary UpsertContact() granted paired")
	}

	paired, err := service.PairAgent(ctx, Contact{
		ContactID:       "tg:2002",
		Kind:            KindAgent,
		Channel:         ChannelTelegram,
		ContactNickname: "Peer Bot",
		TGUsername:      "peer_bot",
		TGPrivateChatID: 2002,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("PairAgent() error = %v", err)
	}
	if !paired.Paired || paired.Kind != KindAgent {
		t.Fatalf("PairAgent() result = %#v", paired)
	}
	if paired.ContactID != "tg:@peer_bot" {
		t.Fatalf("PairAgent() created a duplicate contact: got %q", paired.ContactID)
	}
	if paired.TGPrivateChatID != 2002 {
		t.Fatalf("PairAgent() private chat id = %d, want 2002", paired.TGPrivateChatID)
	}
	if paired.ContactNickname != "Peer" {
		t.Fatalf("PairAgent() overwrote manual nickname: got %q", paired.ContactNickname)
	}

	observed, err := service.UpsertContact(ctx, Contact{
		ContactID:       "tg:@peer_bot",
		Kind:            KindAgent,
		Channel:         ChannelTelegram,
		ContactNickname: "Peer",
		TGUsername:      "peer_bot",
		TGPrivateChatID: 2002,
		Paired:          false,
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpsertContact(after pair) error = %v", err)
	}
	if !observed.Paired {
		t.Fatal("ordinary UpsertContact() cleared paired")
	}
}

func TestPairAgentReplacesOnlyPlaceholderTelegramNickname(t *testing.T) {
	for _, test := range []struct {
		name     string
		current  string
		username string
		want     string
	}{
		{name: "empty", current: "", username: "peer_bot", want: "Peer Bot"},
		{name: "unnamed", current: "Unnamed User", username: "peer_bot", want: "Peer Bot"},
		{name: "username", current: "peer_bot", username: "peer_bot", want: "Peer Bot"},
		{name: "manual", current: "博士的助手", username: "peer_bot", want: "博士的助手"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store := NewFileStore(filepath.Join(t.TempDir(), "contacts"))
			service := NewService(store)
			now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
			if _, err := service.UpsertContact(ctx, Contact{
				ContactID:       "tg:@" + test.username,
				Kind:            KindAgent,
				Channel:         ChannelTelegram,
				ContactNickname: test.current,
				TGUsername:      test.username,
			}, now); err != nil {
				t.Fatalf("UpsertContact() error = %v", err)
			}
			paired, err := service.PairAgent(ctx, Contact{
				ContactID:       "tg:2002",
				Kind:            KindAgent,
				Channel:         ChannelTelegram,
				ContactNickname: "Peer Bot",
				TGUsername:      test.username,
				TGPrivateChatID: 2002,
			}, now.Add(time.Minute))
			if err != nil {
				t.Fatalf("PairAgent() error = %v", err)
			}
			if paired.ContactNickname != test.want {
				t.Fatalf("nickname = %q, want %q", paired.ContactNickname, test.want)
			}
		})
	}
}

func TestFileStorePairedRoundTrip(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)

	if err := store.PutContact(ctx, Contact{
		ContactID:       "slack:T100:U200",
		Kind:            KindAgent,
		Channel:         ChannelSlack,
		ContactNickname: "Peer",
		SlackTeamID:     "T100",
		SlackUserID:     "U200",
		Paired:          true,
	}); err != nil {
		t.Fatalf("PutContact() error = %v", err)
	}

	got, ok, err := store.GetContact(ctx, "slack:T100:U200")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok || !got.Paired {
		t.Fatalf("GetContact() = %#v, %v", got, ok)
	}

	raw, err := os.ReadFile(filepath.Join(root, "ACTIVE.md"))
	if err != nil {
		t.Fatalf("ReadFile(ACTIVE.md) error = %v", err)
	}
	if !strings.Contains(string(raw), "paired: true") {
		t.Fatalf("ACTIVE.md missing paired state:\n%s", raw)
	}
}

func TestFileStorePutContactPreservesPairedAfterStaleRead(t *testing.T) {
	ctx := context.Background()
	store := NewFileStore(filepath.Join(t.TempDir(), "contacts"))
	observed := Contact{
		ContactID:       "tg:@peer_bot",
		Kind:            KindAgent,
		Channel:         ChannelTelegram,
		ContactNickname: "Peer",
		TGUsername:      "peer_bot",
		TGPrivateChatID: 2002,
	}
	if err := store.PutContact(ctx, observed); err != nil {
		t.Fatalf("PutContact(initial) error = %v", err)
	}

	paired := observed
	paired.Paired = true
	if err := store.PutContact(ctx, paired); err != nil {
		t.Fatalf("PutContact(paired) error = %v", err)
	}
	if err := store.PutContact(ctx, observed); err != nil {
		t.Fatalf("PutContact(stale observation) error = %v", err)
	}

	got, ok, err := store.GetContact(ctx, observed.ContactID)
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok || !got.Paired {
		t.Fatalf("stale observation cleared paired state: %#v, found=%v", got, ok)
	}
}

func TestPairAgentReactivatesInactiveContact(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "contacts")
	store := NewFileStore(root)
	service := NewService(store)
	base := time.Date(2026, 8, 17, 11, 0, 0, 0, time.UTC)
	for i := 1; i <= maxActiveContacts+1; i++ {
		lastInteraction := base.Add(time.Duration(i) * time.Minute)
		if err := store.PutContact(ctx, Contact{
			ContactID:         fmt.Sprintf("tg:%d", i),
			Kind:              KindAgent,
			Channel:           ChannelTelegram,
			TGPrivateChatID:   int64(i),
			LastInteractionAt: &lastInteraction,
		}); err != nil {
			t.Fatalf("PutContact(%d) error = %v", i, err)
		}
	}
	if _, ok, err := store.GetContact(ctx, "tg:1"); err != nil || !ok {
		t.Fatalf("GetContact(tg:1) = %v, %v", ok, err)
	}

	paired, err := service.PairAgent(ctx, Contact{
		ContactID:       "tg:1",
		Kind:            KindAgent,
		Channel:         ChannelTelegram,
		TGPrivateChatID: 1,
	}, base.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("PairAgent() error = %v", err)
	}
	if !paired.Paired {
		t.Fatalf("PairAgent() = %#v", paired)
	}
	active, err := store.ListContacts(ctx, StatusActive)
	if err != nil {
		t.Fatalf("ListContacts(active) error = %v", err)
	}
	found := false
	for _, contact := range active {
		if contact.ContactID == "tg:1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("paired contact remained inactive")
	}
}
