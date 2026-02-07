package telegramcmd

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
)

func TestObserveTelegramContact_UpsertByUsername(t *testing.T) {
	ctx := context.Background()
	svc := contacts.NewService(contacts.NewFileStore(filepath.Join(t.TempDir(), "contacts")))
	now := time.Date(2026, 2, 7, 15, 0, 0, 0, time.UTC)

	if err := observeTelegramContact(ctx, svc, 90001, "private", 1001, "alice", "Alice", "", "Alice L", now); err != nil {
		t.Fatalf("observeTelegramContact() error = %v", err)
	}

	item, ok, err := svc.GetContact(ctx, "tg:@alice")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact expected ok=true")
	}
	if item.Kind != contacts.KindHuman {
		t.Fatalf("kind mismatch: got %s want %s", item.Kind, contacts.KindHuman)
	}
	if item.SubjectID != "tg:@alice" {
		t.Fatalf("subject_id mismatch: got %s", item.SubjectID)
	}
	if item.ContactNickname != "Alice L" {
		t.Fatalf("contact_nickname mismatch: got %q", item.ContactNickname)
	}
	if item.LastInteractionAt == nil {
		t.Fatalf("last_interaction_at expected non-nil")
	}
	if len(item.TelegramChats) != 1 || item.TelegramChats[0].ChatID != 90001 || item.TelegramChats[0].ChatType != "private" {
		t.Fatalf("telegram_chats mismatch: got=%v", item.TelegramChats)
	}
}

func TestObserveTelegramContact_FallbackToUserID(t *testing.T) {
	ctx := context.Background()
	svc := contacts.NewService(contacts.NewFileStore(filepath.Join(t.TempDir(), "contacts")))
	now := time.Date(2026, 2, 7, 15, 30, 0, 0, time.UTC)

	if err := observeTelegramContact(ctx, svc, -100778899, "group", 2002, "", "Bob", "", "", now); err != nil {
		t.Fatalf("observeTelegramContact() error = %v", err)
	}

	item, ok, err := svc.GetContact(ctx, "tg:id:2002")
	if err != nil {
		t.Fatalf("GetContact() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContact expected ok=true")
	}
	if item.ContactNickname != "Bob" {
		t.Fatalf("contact_nickname mismatch: got %q", item.ContactNickname)
	}
	if len(item.TelegramChats) != 1 || item.TelegramChats[0].ChatID != -100778899 || item.TelegramChats[0].ChatType != "group" {
		t.Fatalf("telegram_chats mismatch: got=%v", item.TelegramChats)
	}
}
