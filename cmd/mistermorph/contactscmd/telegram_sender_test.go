package contactscmd

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/contactsruntime"
)

func TestResolveTelegramTargetByDecisionChatID(t *testing.T) {
	now := time.Date(2026, 2, 7, 18, 0, 0, 0, time.UTC)
	contact := contacts.Contact{
		ContactID: "tg:id:1001",
		Kind:      contacts.KindHuman,
		TelegramChats: []contacts.TelegramChatRef{
			{ChatID: 1001, ChatType: "private", LastSeenAt: &now},
			{ChatID: -1002233, ChatType: "group", LastSeenAt: &now},
		},
	}
	target, chatType, err := contactsruntime.ResolveTelegramTarget(contact, contacts.ShareDecision{SourceChatID: -1002233})
	if err != nil {
		t.Fatalf("resolveTelegramTarget() error = %v", err)
	}
	got, ok := target.(int64)
	if !ok || got != -1002233 {
		t.Fatalf("target mismatch: got=%T %v", target, target)
	}
	if chatType != "group" {
		t.Fatalf("chat type mismatch: got %q want %q", chatType, "group")
	}
}

func TestResolveTelegramTargetByDecisionChatType(t *testing.T) {
	now := time.Date(2026, 2, 7, 18, 30, 0, 0, time.UTC)
	contact := contacts.Contact{
		ContactID: "tg:id:1001",
		Kind:      contacts.KindHuman,
		TelegramChats: []contacts.TelegramChatRef{
			{ChatID: 1001, ChatType: "private", LastSeenAt: &now},
			{ChatID: -1008899, ChatType: "group", LastSeenAt: &now},
		},
	}
	target, chatType, err := contactsruntime.ResolveTelegramTarget(contact, contacts.ShareDecision{SourceChatType: "group"})
	if err != nil {
		t.Fatalf("resolveTelegramTarget() error = %v", err)
	}
	got, ok := target.(int64)
	if !ok || got != -1008899 {
		t.Fatalf("target mismatch: got=%T %v", target, target)
	}
	if chatType != "group" {
		t.Fatalf("chat type mismatch: got %q want %q", chatType, "group")
	}
}

func TestResolveTelegramTargetFallsBackToPrivate(t *testing.T) {
	now := time.Date(2026, 2, 7, 19, 0, 0, 0, time.UTC)
	contact := contacts.Contact{
		ContactID: "tg:id:1001",
		Kind:      contacts.KindHuman,
		TelegramChats: []contacts.TelegramChatRef{
			{ChatID: -100111, ChatType: "group", LastSeenAt: &now},
			{ChatID: 1001, ChatType: "private", LastSeenAt: &now},
		},
	}
	target, chatType, err := contactsruntime.ResolveTelegramTarget(contact, contacts.ShareDecision{})
	if err != nil {
		t.Fatalf("resolveTelegramTarget() error = %v", err)
	}
	got, ok := target.(int64)
	if !ok || got != 1001 {
		t.Fatalf("target mismatch: got=%T %v", target, target)
	}
	if chatType != "private" {
		t.Fatalf("chat type mismatch: got %q want %q", chatType, "private")
	}
}

func TestResolveTelegramTargetLegacyUsernameFallback(t *testing.T) {
	contact := contacts.Contact{
		ContactID: "tg:@alice",
		Kind:      contacts.KindHuman,
	}
	target, chatType, err := contactsruntime.ResolveTelegramTarget(contact, contacts.ShareDecision{})
	if err != nil {
		t.Fatalf("resolveTelegramTarget() error = %v", err)
	}
	got, ok := target.(string)
	if !ok || got != "@alice" {
		t.Fatalf("target mismatch: got=%T %v", target, target)
	}
	if chatType != "" {
		t.Fatalf("chat type mismatch: got %q want empty", chatType)
	}
}

func TestIsPublicTelegramTarget(t *testing.T) {
	now := time.Date(2026, 2, 7, 19, 30, 0, 0, time.UTC)
	contact := contacts.Contact{
		ContactID: "tg:id:1001",
		Kind:      contacts.KindHuman,
		TelegramChats: []contacts.TelegramChatRef{
			{ChatID: 1001, ChatType: "private", LastSeenAt: &now},
			{ChatID: -100789, ChatType: "group", LastSeenAt: &now},
		},
	}
	if !contactsruntime.IsPublicTelegramTarget(contact, contacts.ShareDecision{SourceChatID: -100789}, int64(-100789), "group") {
		t.Fatalf("expected public target for group chat")
	}
	if contactsruntime.IsPublicTelegramTarget(contact, contacts.ShareDecision{}, int64(1001), "private") {
		t.Fatalf("did not expect public target for private chat")
	}
}
