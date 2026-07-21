package awareness

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
)

var errAwarenessTestBoom = errors.New("boom")

func TestBuildCronNotifyTargetForTaskFetchesChatProfile(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	store := chatinfo.NewStore(t.TempDir())
	task := cronstore.Task{
		ChatID:  "tg:-100123",
		Content: "Remind [Alice](tg:@alice).",
	}
	target := buildCronNotifyTargetForTask(ctx, task, now, store, chatinfo.RefreshFunc(func(_ context.Context, chatID string) (chatinfo.Info, error) {
		return chatinfo.Info{
			ChatID:   chatID,
			Platform: "telegram",
			Type:     "supergroup",
			Name:     "Project Room",
		}, nil
	}), nil)
	if target == nil {
		t.Fatalf("target is nil")
	}
	chatProfile, ok := target["chat_profile"].(map[string]string)
	if !ok {
		t.Fatalf("chat_profile type = %T", target["chat_profile"])
	}
	if chatProfile["name"] != "Project Room" {
		t.Fatalf("chat_profile = %#v", chatProfile)
	}
}

func TestBuildCronNotifyTargetForTaskKeepsChatIDOnFetchFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	store := chatinfo.NewStore(t.TempDir())
	task := cronstore.Task{
		ChatID:  "tg:-100123",
		Content: "Send status.",
	}
	target := buildCronNotifyTargetForTask(ctx, task, now, store, chatinfo.RefreshFunc(func(context.Context, string) (chatinfo.Info, error) {
		return chatinfo.Info{}, errAwarenessTestBoom
	}), nil)
	if target == nil {
		t.Fatalf("target is nil")
	}
	if target["chat_id"] != "tg:-100123" {
		t.Fatalf("chat_id = %#v", target["chat_id"])
	}
	if _, ok := target["chat_profile"]; ok {
		t.Fatalf("chat_profile should be absent: %#v", target)
	}
}

func TestRefreshChatInfoOnStartFetchesCandidatesWhenStoreEmpty(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	contactsDir := filepath.Join(stateDir, "contacts")
	if err := contacts.NewFileStore(contactsDir).PutContact(ctx, contacts.Contact{
		ContactID:      "tg:@alice",
		Kind:           contacts.KindHuman,
		Channel:        contacts.ChannelTelegram,
		TGGroupChatIDs: []int64{-100123},
	}); err != nil {
		t.Fatalf("seed active contact: %v", err)
	}
	store := chatinfo.NewStore(contactsDir)

	refreshChatInfoOnStart(ctx, store, chatinfo.RefreshFunc(func(_ context.Context, chatID string) (chatinfo.Info, error) {
		return chatinfo.Info{
			ChatID:   chatID,
			Platform: "telegram",
			Type:     "supergroup",
			Name:     "Project Room",
		}, nil
	}), contactsDir, nil)

	items, exists, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("store.Read() error = %v", err)
	}
	if !exists || len(items) != 1 {
		t.Fatalf("items = %#v exists=%v, want one item", items, exists)
	}
	if items[0].ChatID != "tg:-100123" || items[0].Name != "Project Room" {
		t.Fatalf("unexpected item: %#v", items[0])
	}
}

func TestResolveChatInfoRuntimeDoesNotReadProcessGlobalConfiguration(t *testing.T) {
	store, refresher := resolveChatInfoRuntime(RunOptions{})
	if store != nil {
		t.Fatalf("store = %#v, want nil", store)
	}
	if refresher != nil {
		t.Fatalf("refresher = %#v, want explicit dependency", refresher)
	}
}

func TestRefreshChatInfoOnStartSkipsMissingCandidatesWhenStoreHasItems(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	contactsDir := filepath.Join(stateDir, "contacts")
	if err := contacts.NewFileStore(contactsDir).PutContact(ctx, contacts.Contact{
		ContactID:      "tg:@alice",
		Kind:           contacts.KindHuman,
		Channel:        contacts.ChannelTelegram,
		TGGroupChatIDs: []int64{-100456},
	}); err != nil {
		t.Fatalf("seed active contact: %v", err)
	}
	store := chatinfo.NewStore(contactsDir)
	if err := store.Write(ctx, []chatinfo.Info{
		{
			ChatID:    "tg:-100123",
			Platform:  "telegram",
			Type:      "supergroup",
			Name:      "Existing Room",
			FetchedAt: time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC),
			ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		},
	}); err != nil {
		t.Fatalf("seed chat profile: %v", err)
	}
	called := false

	refreshChatInfoOnStart(ctx, store, chatinfo.RefreshFunc(func(context.Context, string) (chatinfo.Info, error) {
		called = true
		return chatinfo.Info{}, errAwarenessTestBoom
	}), contactsDir, nil)

	if called {
		t.Fatalf("refresher should not be called when store already has items")
	}
	items, _, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("store.Read() error = %v", err)
	}
	if len(items) != 1 || items[0].ChatID != "tg:-100123" {
		t.Fatalf("unexpected items: %#v", items)
	}
}
