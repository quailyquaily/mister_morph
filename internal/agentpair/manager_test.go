package agentpair

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

func TestManagersPairAfterReciprocalAdminRequests(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	peerA := telegramPeer(1001, "agent_a", "Agent A")
	peerB := telegramPeer(2002, "agent_b", "Agent B")
	var offerA, offerB string
	managerA, storeA, journalA := newTestManager(t, peerA, []string{"tg:11"}, clock, func(_ context.Context, target Peer, body string) error {
		if target.ID != "tg:@agent_b" {
			t.Fatalf("A send target = %q", target.ID)
		}
		offerA = body
		return nil
	})
	managerB, storeB, journalB := newTestManager(t, peerB, []string{"tg:22"}, clock, func(_ context.Context, target Peer, body string) error {
		if target.ID != "tg:@agent_a" {
			t.Fatalf("B send target = %q", target.ID)
		}
		offerB = body
		return nil
	})

	status, err := managerA.Start(ctx, "tg:11", telegramAliasPeer("agent_b"), "")
	if err != nil {
		t.Fatalf("managerA.Start() error = %v", err)
	}
	if status != StatusWaiting || offerA == "" {
		t.Fatalf("managerA.Start() = %q, offer=%q", status, offerA)
	}

	status, handled, err := managerB.Handle(ctx, peerA, offerA)
	if err != nil {
		t.Fatalf("managerB.Handle(A offer) error = %v", err)
	}
	if !handled || status != StatusWaiting {
		t.Fatalf("managerB.Handle(A offer) = %q, handled=%v", status, handled)
	}
	if got := journalEventTypes(t, journalB); len(got) != 0 {
		t.Fatalf("unsolicited offer wrote journal events: %v", got)
	}

	status, err = managerB.Start(ctx, "tg:22", telegramAliasPeer("agent_a"), "")
	if err != nil {
		t.Fatalf("managerB.Start() error = %v", err)
	}
	if status != StatusCompleted || offerB == "" {
		t.Fatalf("managerB.Start() = %q, offer=%q", status, offerB)
	}

	status, handled, err = managerA.Handle(ctx, peerB, offerB)
	if err != nil {
		t.Fatalf("managerA.Handle(B offer) error = %v", err)
	}
	if !handled || status != StatusCompleted {
		t.Fatalf("managerA.Handle(B offer) = %q, handled=%v", status, handled)
	}

	assertPairedTelegramContact(t, storeA, "agent_b", 2002)
	assertPairedTelegramContact(t, storeB, "agent_a", 1001)
	assertEventTypes(t, journalA, []string{"agent_pair_requested", "agent_pair_completed"})
	assertEventTypes(t, journalB, []string{"agent_pair_requested", "agent_pair_completed"})

	status, handled, err = managerA.Handle(ctx, peerB, offerB)
	if err != nil {
		t.Fatalf("managerA.Handle(replay) error = %v", err)
	}
	if !handled || status != StatusAlreadyPaired {
		t.Fatalf("managerA.Handle(replay) = %q, handled=%v", status, handled)
	}
	assertEventTypes(t, journalA, []string{"agent_pair_requested", "agent_pair_completed"})
}

func TestMixinPeerReferencesUseUserIDAndIdentityNumber(t *testing.T) {
	peer := Peer{
		ID: "mixin:11111111-1111-1111-1111-111111111111",
		Contact: contacts.Contact{
			ContactID: "mixin:11111111-1111-1111-1111-111111111111", Channel: contacts.ChannelMixin,
			MixinUserID: "11111111-1111-1111-1111-111111111111", MixinIdentityNumber: "7000123456",
		},
	}
	if !peerHasReference(peer, "mixin:@7000123456") || channelForReference(peer.ID) != contacts.ChannelMixin {
		t.Fatalf("Mixin peer references = %#v", peerKeys(peer))
	}
}

func TestManagerReportsCompletedWhenCompletionJournalFails(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 8, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	peerA := telegramPeer(1001, "agent_a", "Agent A")
	peerB := telegramPeer(2002, "agent_b", "Agent B")
	var offerA, offerB string
	managerA, storeA, _ := newTestManager(t, peerA, []string{"tg:11"}, clock, func(_ context.Context, _ Peer, body string) error {
		offerA = body
		return nil
	})
	managerB, _, _ := newTestManager(t, peerB, []string{"tg:22"}, clock, func(_ context.Context, _ Peer, body string) error {
		offerB = body
		return nil
	})

	if _, err := managerA.Start(ctx, "tg:11", telegramAliasPeer("agent_b"), ""); err != nil {
		t.Fatalf("managerA.Start() error = %v", err)
	}
	if _, handled, err := managerB.Handle(ctx, peerA, offerA); err != nil || !handled {
		t.Fatalf("managerB.Handle(A offer) = handled %v, error %v", handled, err)
	}
	if _, err := managerB.Start(ctx, "tg:22", telegramAliasPeer("agent_a"), ""); err != nil {
		t.Fatalf("managerB.Start() error = %v", err)
	}
	if err := managerA.journal.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	status, handled, err := managerA.Handle(ctx, peerB, offerB)
	if err != nil {
		t.Fatalf("managerA.Handle(B offer) error = %v", err)
	}
	if !handled || status != StatusCompleted {
		t.Fatalf("managerA.Handle(B offer) = %q, handled=%v", status, handled)
	}
	assertPairedTelegramContact(t, storeA, "agent_b", 2002)
}

func TestManagerRejectsNonAdminWithoutJournal(t *testing.T) {
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	sends := 0
	manager, _, journalDir := newTestManager(t, telegramPeer(1001, "agent_a", "Agent A"), []string{"tg:11"}, func() time.Time { return now }, func(context.Context, Peer, string) error {
		sends++
		return nil
	})

	if _, err := manager.Start(context.Background(), "tg:12", telegramAliasPeer("agent_b"), ""); err == nil {
		t.Fatal("Start(non-admin) expected error")
	}
	if sends != 0 {
		t.Fatalf("send calls = %d, want 0", sends)
	}
	if got := journalEventTypes(t, journalDir); len(got) != 0 {
		t.Fatalf("non-admin request wrote journal events: %v", got)
	}
}

func TestManagerAuthorizesTelegramAdminContactReference(t *testing.T) {
	now := time.Date(2026, 8, 17, 9, 15, 0, 0, time.UTC)
	tests := []struct {
		name          string
		contactUserID int64
		addContact    bool
		wantError     bool
	}{
		{name: "matching contact", contactUserID: 11, addContact: true},
		{name: "contact without stored numeric id", addContact: true},
		{name: "contact missing", wantError: true},
		{name: "contact reference is sufficient", contactUserID: 12, addContact: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sends := 0
			manager, store, journalDir := newTestManager(t, telegramPeer(1001, "agent_a", "Agent A"), []string{"tg:@ballcatcat"}, func() time.Time { return now }, func(context.Context, Peer, string) error {
				sends++
				return nil
			})
			if tt.addContact {
				if err := store.PutContact(context.Background(), contacts.Contact{
					ContactID:       "tg:@ballcatcat",
					Kind:            contacts.KindHuman,
					Channel:         contacts.ChannelTelegram,
					ContactNickname: "Admin",
					TGUsername:      "ballcatcat",
					TGPrivateChatID: tt.contactUserID,
				}); err != nil {
					t.Fatalf("PutContact() error = %v", err)
				}
			}

			_, err := manager.Start(context.Background(), "tg:11", telegramAliasPeer("agent_b"), "tg:@ballcatcat")
			if (err != nil) != tt.wantError {
				t.Fatalf("Start() error = %v, wantError %v", err, tt.wantError)
			}
			wantSends := 1
			wantEvents := 1
			if tt.wantError {
				wantSends = 0
				wantEvents = 0
			}
			if sends != wantSends {
				t.Fatalf("send calls = %d, want %d", sends, wantSends)
			}
			if events := journalEventTypes(t, journalDir); len(events) != wantEvents {
				t.Fatalf("journal events = %v, want %d", events, wantEvents)
			}
		})
	}
}

func TestManagerRejectsPairingWithItself(t *testing.T) {
	now := time.Date(2026, 8, 17, 9, 30, 0, 0, time.UTC)
	sends := 0
	manager, _, journalDir := newTestManager(t, telegramPeer(1001, "agent_a", "Agent A"), []string{"tg:11"}, func() time.Time { return now }, func(context.Context, Peer, string) error {
		sends++
		return nil
	})
	if _, err := manager.Start(context.Background(), "tg:11", telegramAliasPeer("agent_a"), ""); err == nil {
		t.Fatal("Start(self) expected error")
	}
	if sends != 0 {
		t.Fatalf("send calls = %d, want 0", sends)
	}
	if got := journalEventTypes(t, journalDir); len(got) != 0 {
		t.Fatalf("self-pair request wrote journal events: %v", got)
	}
}

func TestManagerExpiresLocalIntentBeforeLateOffer(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	peerA := telegramPeer(1001, "agent_a", "Agent A")
	peerB := telegramPeer(2002, "agent_b", "Agent B")
	var offerB string
	managerA, storeA, journalA := newTestManager(t, peerA, []string{"tg:11"}, clock, func(context.Context, Peer, string) error { return nil })
	managerB, _, _ := newTestManager(t, peerB, []string{"tg:22"}, clock, func(_ context.Context, _ Peer, body string) error {
		offerB = body
		return nil
	})

	if _, err := managerA.Start(ctx, "tg:11", telegramAliasPeer("agent_b"), ""); err != nil {
		t.Fatalf("managerA.Start() error = %v", err)
	}
	now = now.Add(6 * time.Minute)
	if _, err := managerB.Start(ctx, "tg:22", telegramAliasPeer("agent_a"), ""); err != nil {
		t.Fatalf("managerB.Start() error = %v", err)
	}
	status, handled, err := managerA.Handle(ctx, peerB, offerB)
	if err != nil {
		t.Fatalf("managerA.Handle(late offer) error = %v", err)
	}
	if !handled || status != StatusWaiting {
		t.Fatalf("managerA.Handle(late offer) = %q, handled=%v", status, handled)
	}
	if paired, err := managerA.IsPaired(ctx, peerB); err != nil || paired {
		t.Fatalf("managerA.IsPaired() = %v, %v", paired, err)
	}
	if contactsList, err := storeA.ListContacts(ctx, contacts.StatusActive); err != nil {
		t.Fatalf("ListContacts() error = %v", err)
	} else if len(contactsList) != 0 {
		t.Fatalf("late offer created contacts: %#v", contactsList)
	}
	assertEventTypes(t, journalA, []string{"agent_pair_requested", "agent_pair_expired"})
}

func TestManagerRecordsExpirationWithoutAnotherMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	store := contacts.NewFileStore(filepath.Join(root, "contacts"))
	admins, err := ParseAdmins([]string{"tg:11"})
	if err != nil {
		t.Fatalf("ParseAdmins() error = %v", err)
	}
	journalDir := filepath.Join(root, "journal")
	manager, err := New(Options{
		Context:    ctx,
		Self:       telegramPeer(1001, "agent_a", "Agent A"),
		Admins:     admins,
		Contacts:   contacts.NewService(store),
		JournalDir: journalDir,
		TTL:        30 * time.Millisecond,
		Send:       func(context.Context, Peer, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := manager.Start(ctx, "tg:11", telegramAliasPeer("agent_b"), ""); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got := journalEventTypes(t, journalDir)
		if len(got) == 2 {
			assertEventTypes(t, journalDir, []string{"agent_pair_requested", "agent_pair_expired"})
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expiration was not journaled: %v", journalEventTypes(t, journalDir))
}

func newTestManager(t *testing.T, self Peer, adminIDs []string, now func() time.Time, send SendFunc) (*Manager, *contacts.FileStore, string) {
	t.Helper()
	root := t.TempDir()
	store := contacts.NewFileStore(filepath.Join(root, "contacts"))
	admins, err := ParseAdmins(adminIDs)
	if err != nil {
		t.Fatalf("ParseAdmins() error = %v", err)
	}
	journalDir := filepath.Join(root, "journal")
	manager, err := New(Options{
		Self:       self,
		Admins:     admins,
		Contacts:   contacts.NewService(store),
		JournalDir: journalDir,
		Now:        now,
		Send:       send,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return manager, store, journalDir
}

func telegramPeer(id int64, username, nickname string) Peer {
	return Peer{
		ID: "tg:" + strconv.FormatInt(id, 10),
		Contact: contacts.Contact{
			ContactID:       "tg:@" + username,
			Kind:            contacts.KindAgent,
			Channel:         contacts.ChannelTelegram,
			ContactNickname: nickname,
			TGUsername:      username,
			TGPrivateChatID: id,
		},
	}
}

func telegramAliasPeer(username string) Peer {
	return Peer{
		ID: "tg:@" + username,
		Contact: contacts.Contact{
			ContactID:  "tg:@" + username,
			Kind:       contacts.KindAgent,
			Channel:    contacts.ChannelTelegram,
			TGUsername: username,
		},
	}
}

func assertPairedTelegramContact(t *testing.T, store *contacts.FileStore, username string, privateChatID int64) {
	t.Helper()
	items, err := store.ListContacts(context.Background(), contacts.StatusActive)
	if err != nil {
		t.Fatalf("ListContacts() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("active contacts = %#v", items)
	}
	if !items[0].Paired || items[0].Kind != contacts.KindAgent || items[0].TGUsername != username || items[0].TGPrivateChatID != privateChatID {
		t.Fatalf("paired contact = %#v", items[0])
	}
}

func assertEventTypes(t *testing.T, dir string, want []string) {
	t.Helper()
	got := journalEventTypes(t, dir)
	if len(got) != len(want) {
		t.Fatalf("journal events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("journal events = %v, want %v", got, want)
		}
	}
}

func journalEventTypes(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	if err := domainjournal.ReplayDir(dir, func(record domainjournal.Record) error {
		if record.Event.Domain == "contacts" {
			out = append(out, record.Event.Type)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	return out
}
