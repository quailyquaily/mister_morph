package chatinfo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

var errBoom = errors.New("boom")

func TestStoreReadWriteRoundTripSorted(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)

	items := []Info{
		{
			ChatID:    "slack:T1:C9",
			Platform:  "slack",
			Type:      "channel",
			Name:      "Ops",
			FetchedAt: now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
		},
		{
			ChatID:    "tg:-100123",
			Platform:  "telegram",
			Type:      "supergroup",
			Name:      "Project",
			FetchedAt: now,
			ExpiresAt: now.Add(7 * 24 * time.Hour),
		},
	}
	if err := store.Write(ctx, items); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, exists, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !exists {
		t.Fatalf("Read() exists = false")
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].ChatID != "slack:T1:C9" || got[1].ChatID != "tg:-100123" {
		t.Fatalf("items not sorted by chat_id: %#v", got)
	}
}

func TestStoreReadDropsLegacyAvatarRef(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root)
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	raw := map[string]any{
		"version": fileVersion,
		"items": []map[string]any{
			{
				"chat_id":    "tg:-100123",
				"platform":   "telegram",
				"name":       "Project",
				"avatar_ref": "telegram:file_id:abc",
				"fetched_at": now,
				"expires_at": now.Add(7 * 24 * time.Hour),
			},
		},
	}
	if err := fsstore.WriteJSONAtomic(store.Path(), raw, fsstore.FileOptions{DirPerm: 0o700, FilePerm: 0o600}); err != nil {
		t.Fatalf("write raw chat profile: %v", err)
	}

	got, exists, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !exists || len(got) != 1 {
		t.Fatalf("unexpected read: exists=%v items=%#v", exists, got)
	}
	encoded, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal item: %v", err)
	}
	if strings.Contains(string(encoded), "avatar_ref") {
		t.Fatalf("chat profile should not expose avatar_ref: %s", encoded)
	}
	stored, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if strings.Contains(string(stored), "avatar_ref") {
		t.Fatalf("stored chat profile should drop avatar_ref: %s", stored)
	}
}

func TestStoreRefreshExpiredKeepsOldInfoOnFailure(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	old := Info{
		ChatID:    "tg:-100123",
		Platform:  "telegram",
		Type:      "supergroup",
		Name:      "Old Name",
		FetchedAt: now.Add(-8 * 24 * time.Hour),
		ExpiresAt: now.Add(-time.Minute),
	}
	if err := store.Write(ctx, []Info{old}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	calls := 0
	refresher := RefreshFunc(func(context.Context, string) (Info, error) {
		calls++
		return Info{}, errBoom
	})
	if err := store.RefreshExpired(ctx, now, refresher); err != nil {
		t.Fatalf("RefreshExpired() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls)
	}
	got, _, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Name != "Old Name" {
		t.Fatalf("name = %q, want old name", got[0].Name)
	}
	if !got[0].ExpiresAt.After(now) || got[0].ExpiresAt.After(now.Add(7*time.Hour)) {
		t.Fatalf("retry expires_at = %s, want short retry after now", got[0].ExpiresAt)
	}
}

func TestStoreGetLazyFetchesMissingAndCaches(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	fetched := Info{
		ChatID:   "tg:-100123",
		Platform: "telegram",
		Type:     "supergroup",
		Name:     "Project",
	}

	calls := 0
	info, ok, err := store.Get(ctx, now, "tg:-100123", RefreshFunc(func(_ context.Context, chatID string) (Info, error) {
		calls++
		if chatID != "tg:-100123" {
			t.Fatalf("chatID = %q, want tg:-100123", chatID)
		}
		return fetched, nil
	}))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatalf("Get() ok = false")
	}
	if info.Name != "Project" {
		t.Fatalf("name = %q, want Project", info.Name)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}

	info, ok, err = store.Get(ctx, now.Add(time.Hour), "tg:-100123", RefreshFunc(func(context.Context, string) (Info, error) {
		t.Fatalf("unexpected refresh call")
		return Info{}, nil
	}))
	if err != nil {
		t.Fatalf("Get(cached) error = %v", err)
	}
	if !ok || info.Name != "Project" {
		t.Fatalf("cached info mismatch: ok=%v info=%#v", ok, info)
	}
}

func TestStorePath(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	if got := store.Path(); got != filepath.Join(root, Filename) {
		t.Fatalf("Path() = %q", got)
	}
}

func TestStoreReadMigratesLegacyChatIDInfoFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root)
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	legacy := File{
		Version: fileVersion,
		Items: []Info{
			{
				ChatID:    "tg:-100123",
				Platform:  "telegram",
				Name:      "Project",
				FetchedAt: now,
				ExpiresAt: now.Add(7 * 24 * time.Hour),
			},
		},
	}
	if err := fsstore.WriteJSONAtomic(filepath.Join(root, LegacyFilename), legacy, fsstore.FileOptions{DirPerm: 0o700, FilePerm: 0o600}); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	got, exists, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !exists || len(got) != 1 || got[0].ChatID != "tg:-100123" {
		t.Fatalf("unexpected legacy read: exists=%v items=%#v", exists, got)
	}
	if _, err := os.Stat(filepath.Join(root, Filename)); err != nil {
		t.Fatalf("new chat profile file should be written: %v", err)
	}
}

func TestStorePutMixinChatProfile(t *testing.T) {
	store := NewStore(t.TempDir())
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	chatID := "mixin:11111111-1111-1111-1111-111111111111"
	if err := store.Put(context.Background(), Info{ChatID: chatID, Type: "group", Name: "Morphs", FetchedAt: now}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	items, exists, err := store.Read(context.Background())
	if err != nil || !exists || len(items) != 1 {
		t.Fatalf("Read() = %#v, %v, %v", items, exists, err)
	}
	if items[0].ChatID != chatID || items[0].Platform != "mixin" || items[0].Name != "Morphs" {
		t.Fatalf("item = %#v", items[0])
	}
}
