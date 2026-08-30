package contacts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestContactAvatarRefresherRefreshesAndDeduplicates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewFileStore(t.TempDir())
	if err := store.PutContact(ctx, Contact{ContactID: "tg:1", Channel: ChannelTelegram, ContactNickname: "Alice"}); err != nil {
		t.Fatalf("PutContact() error = %v", err)
	}
	refresher, err := NewContactAvatarRefresher(ctx, store, nil)
	if err != nil {
		t.Fatalf("NewContactAvatarRefresher() error = %v", err)
	}
	defer refresher.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	fetch := func(context.Context) ([]byte, bool, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return testContactAvatarPNG(t), true, nil
	}
	if !refresher.Enqueue("tg:1", fetch) {
		t.Fatal("first Enqueue() = false, want true")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("avatar fetch did not start")
	}
	if refresher.Enqueue("tg:1", fetch) {
		t.Fatal("duplicate Enqueue() = true, want false")
	}
	close(release)
	waitContactAvatarRefresherIdle(t, refresher)

	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
	if _, found, err := store.ReadContactAvatar(context.Background(), "tg:1"); err != nil || !found {
		t.Fatalf("cached avatar = (found=%v, err=%v), want true, nil", found, err)
	}
}

func TestContactAvatarRefresherCacheRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		prepare       func(*testing.T, *FileStore, string)
		fetch         ContactAvatarFetchFunc
		wantCalls     int32
		wantAvatar    bool
		wantUnchanged bool
	}{
		{
			name: "fresh cache skips fetch",
			prepare: func(t *testing.T, store *FileStore, contactID string) {
				if err := store.PutContactAvatar(context.Background(), contactID, testContactAvatarPNG(t)); err != nil {
					t.Fatalf("prepare avatar: %v", err)
				}
			},
			wantAvatar: true,
		},
		{
			name:    "no avatar removes stale cache",
			prepare: prepareStaleContactAvatar,
			fetch: func(context.Context) ([]byte, bool, error) {
				return nil, false, nil
			},
			wantCalls:  1,
			wantAvatar: false,
		},
		{
			name:    "fetch error retains stale cache",
			prepare: prepareStaleContactAvatar,
			fetch: func(context.Context) ([]byte, bool, error) {
				return nil, false, errors.New("rate limited")
			},
			wantCalls:     1,
			wantAvatar:    true,
			wantUnchanged: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := NewFileStore(t.TempDir())
			contactID := "tg:1"
			if test.prepare != nil {
				test.prepare(t, store, contactID)
			}
			before, beforeFound, err := store.ReadContactAvatar(ctx, contactID)
			if err != nil {
				t.Fatalf("read before: %v", err)
			}

			refresher, err := NewContactAvatarRefresher(ctx, store, nil)
			if err != nil {
				t.Fatalf("NewContactAvatarRefresher() error = %v", err)
			}
			defer refresher.Close()
			var calls atomic.Int32
			fetch := test.fetch
			if fetch == nil {
				fetch = func(context.Context) ([]byte, bool, error) {
					return testContactAvatarPNG(t), true, nil
				}
			}
			if !refresher.Enqueue(contactID, func(ctx context.Context) ([]byte, bool, error) {
				calls.Add(1)
				return fetch(ctx)
			}) {
				t.Fatal("Enqueue() = false, want true")
			}
			waitContactAvatarRefresherIdle(t, refresher)

			if got := calls.Load(); got != test.wantCalls {
				t.Fatalf("fetch calls = %d, want %d", got, test.wantCalls)
			}
			after, found, err := store.ReadContactAvatar(ctx, contactID)
			if err != nil {
				t.Fatalf("read after: %v", err)
			}
			if found != test.wantAvatar {
				t.Fatalf("avatar found = %v, want %v", found, test.wantAvatar)
			}
			if test.wantUnchanged && (!beforeFound || string(before.Data) != string(after.Data)) {
				t.Fatal("stale avatar changed after fetch error")
			}
		})
	}
}

func TestContactAvatarRefresherRemembersMissingAvatar(t *testing.T) {
	store := NewFileStore(t.TempDir())
	refresher, err := NewContactAvatarRefresher(context.Background(), store, nil)
	if err != nil {
		t.Fatalf("NewContactAvatarRefresher() error = %v", err)
	}
	defer refresher.Close()

	var calls atomic.Int32
	fetch := func(context.Context) ([]byte, bool, error) {
		calls.Add(1)
		return nil, false, nil
	}
	if !refresher.Enqueue("tg:missing", fetch) {
		t.Fatal("first Enqueue() = false")
	}
	waitContactAvatarRefresherIdle(t, refresher)
	if refresher.Enqueue("tg:missing", fetch) {
		t.Fatal("second Enqueue() = true, want remembered missing avatar")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1", got)
	}
}

func TestContactAvatarRefresherPrewarmsMatchingChannelContacts(t *testing.T) {
	ctx := context.Background()
	store := NewFileStore(t.TempDir())
	for _, contact := range []Contact{
		{ContactID: "tg:1", Channel: ChannelTelegram, ContactNickname: "Alice"},
		{ContactID: "slack:T1:U1", Channel: ChannelSlack, ContactNickname: "Bob"},
	} {
		if err := store.PutContact(ctx, contact); err != nil {
			t.Fatalf("PutContact(%q) error = %v", contact.ContactID, err)
		}
	}
	refresher, err := NewContactAvatarRefresher(ctx, store, nil)
	if err != nil {
		t.Fatalf("NewContactAvatarRefresher() error = %v", err)
	}
	defer refresher.Close()

	if err := refresher.Prewarm(ChannelTelegram, func(Contact) ContactAvatarFetchFunc {
		return func(context.Context) ([]byte, bool, error) {
			return testContactAvatarPNG(t), true, nil
		}
	}); err != nil {
		t.Fatalf("Prewarm() error = %v", err)
	}
	waitContactAvatarRefresherIdle(t, refresher)

	if _, found, err := store.ReadContactAvatar(ctx, "tg:1"); err != nil || !found {
		t.Fatalf("telegram avatar = (found=%v, err=%v), want true, nil", found, err)
	}
	if _, found, err := store.ReadContactAvatar(ctx, "slack:T1:U1"); err != nil || found {
		t.Fatalf("slack avatar = (found=%v, err=%v), want false, nil", found, err)
	}
}

func TestContactAvatarRefresherPrunesExpiredMissingAvatars(t *testing.T) {
	store := NewFileStore(t.TempDir())
	refresher, err := NewContactAvatarRefresher(context.Background(), store, nil)
	if err != nil {
		t.Fatalf("NewContactAvatarRefresher() error = %v", err)
	}
	defer refresher.Close()

	now := time.Now().UTC()
	refresher.mu.Lock()
	refresher.missing["tg:expired"] = now.Add(-time.Minute)
	refresher.missing["tg:fresh"] = now.Add(time.Minute)
	refresher.mu.Unlock()

	refresher.pruneMissing(now)

	refresher.mu.Lock()
	_, expiredExists := refresher.missing["tg:expired"]
	_, freshExists := refresher.missing["tg:fresh"]
	refresher.mu.Unlock()
	if expiredExists || !freshExists {
		t.Fatalf("missing cache after prune = expired:%v fresh:%v", expiredExists, freshExists)
	}
}

func TestContactAvatarRefresherDoesNotRecreateDeletedContactAvatar(t *testing.T) {
	ctx := context.Background()
	store := NewFileStore(t.TempDir())
	if err := store.PutContact(ctx, Contact{
		ContactID:       "tg:1",
		Channel:         ChannelTelegram,
		ContactNickname: "Alice",
	}); err != nil {
		t.Fatalf("PutContact() error = %v", err)
	}
	refresher, err := NewContactAvatarRefresher(ctx, store, nil)
	if err != nil {
		t.Fatalf("NewContactAvatarRefresher() error = %v", err)
	}
	defer refresher.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	if !refresher.Enqueue("tg:1", func(context.Context) ([]byte, bool, error) {
		close(started)
		<-release
		return testContactAvatarPNG(t), true, nil
	}) {
		t.Fatal("Enqueue() = false")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("avatar fetch did not start")
	}
	if _, deleted, err := store.DeleteContactYAML(ctx, "tg:1"); err != nil || !deleted {
		t.Fatalf("DeleteContactYAML() = (deleted=%v, err=%v)", deleted, err)
	}
	close(release)
	waitContactAvatarRefresherIdle(t, refresher)

	if _, found, err := store.ReadContactAvatar(ctx, "tg:1"); err != nil || found {
		t.Fatalf("avatar after contact delete = (found=%v, err=%v), want false, nil", found, err)
	}
}

func TestFetchContactAvatarURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testContactAvatarPNG(t))
	}))
	defer server.Close()

	raw, found, err := FetchContactAvatarURL(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("FetchContactAvatarURL() error = %v", err)
	}
	if !found || string(raw) != string(testContactAvatarPNG(t)) {
		t.Fatalf("FetchContactAvatarURL() = (len=%d, found=%v), want png, true", len(raw), found)
	}

	if _, found, err := FetchContactAvatarURL(context.Background(), server.Client(), ""); err != nil || found {
		t.Fatalf("empty URL = (found=%v, err=%v), want false, nil", found, err)
	}
	if _, _, err := FetchContactAvatarURL(context.Background(), server.Client(), "http://example.test/avatar.png"); err == nil {
		t.Fatal("HTTP avatar URL error = nil, want scheme error")
	}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = FetchContactAvatarURL(canceledCtx, server.Client(), "https://cdn.example/avatar.png?signed=secret-value")
	if err == nil {
		t.Fatal("canceled avatar request error = nil")
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("avatar request error exposes URL query: %v", err)
	}
}

func prepareStaleContactAvatar(t *testing.T, store *FileStore, contactID string) {
	t.Helper()
	if err := store.PutContactAvatar(context.Background(), contactID, testContactAvatarPNG(t)); err != nil {
		t.Fatalf("prepare avatar: %v", err)
	}
	old := time.Now().UTC().Add(-ContactAvatarTTL - time.Hour)
	if err := os.Chtimes(store.contactAvatarPath(contactID), old, old); err != nil {
		t.Fatalf("age avatar: %v", err)
	}
}

func waitContactAvatarRefresherIdle(t *testing.T, refresher *ContactAvatarRefresher) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		refresher.mu.Lock()
		pending := len(refresher.pending)
		refresher.mu.Unlock()
		if pending == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("contact avatar refresher did not become idle")
}
