package contactsruntime

import (
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/contacts"
	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
)

func TestResolveTelegramTargetPrefersPrivate(t *testing.T) {
	contact := contacts.Contact{
		ContactID:       "tg:1001",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 1001,
		TGGroupChatIDs:  []int64{-1002233},
	}
	target, chatType, err := ResolveTelegramTarget(contact)
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

func TestResolveTelegramTargetFallsBackToGroup(t *testing.T) {
	contact := contacts.Contact{
		ContactID:       "tg:1001",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 0,
		TGGroupChatIDs:  []int64{-1008899},
	}
	target, chatType, err := ResolveTelegramTarget(contact)
	if err != nil {
		t.Fatalf("resolveTelegramTarget() error = %v", err)
	}
	got, ok := target.(int64)
	if !ok || got != -1008899 {
		t.Fatalf("target mismatch: got=%T %v", target, target)
	}
	if chatType != "supergroup" {
		t.Fatalf("chat type mismatch: got %q want %q", chatType, "supergroup")
	}
}

func TestResolveTelegramTargetFallsBackToPrivate(t *testing.T) {
	contact := contacts.Contact{
		ContactID:       "tg:1001",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 1001,
		TGGroupChatIDs:  []int64{-100111},
	}
	target, chatType, err := ResolveTelegramTarget(contact)
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

func TestResolveTelegramTargetUsesUsername(t *testing.T) {
	target, chatType, err := ResolveTelegramTarget(contacts.Contact{
		ContactID:  "tg:@smith_bot",
		Kind:       contacts.KindAgent,
		Channel:    contacts.ChannelTelegram,
		TGUsername: "smith_bot",
	})
	if err != nil {
		t.Fatalf("ResolveTelegramTarget() error = %v", err)
	}
	if target != "@smith_bot" {
		t.Fatalf("target = %#v, want %q", target, "@smith_bot")
	}
	if chatType != "private" {
		t.Fatalf("chat type = %q, want %q", chatType, "private")
	}
	username, ok, err := parseTelegramUsernameTarget(target)
	if err != nil || !ok || username != "@smith_bot" {
		t.Fatalf("parseTelegramUsernameTarget(%#v) = (%q, %v, %v)", target, username, ok, err)
	}
	if _, ok, err := parseTelegramUsernameTarget("tg:@smith_bot"); err != nil || ok {
		t.Fatalf("raw contact reference accepted as delivery target: ok=%v err=%v", ok, err)
	}
}

func TestResolveTelegramTargetWithChatIDMatchGroup(t *testing.T) {
	contact := contacts.Contact{
		ContactID:       "tg:@alice",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 1001,
		TGGroupChatIDs:  []int64{-100111},
	}
	target, chatType, err := ResolveTelegramTargetWithChatID(contact, "tg:-100111")
	if err != nil {
		t.Fatalf("ResolveTelegramTargetWithChatID() error = %v", err)
	}
	got, ok := target.(int64)
	if !ok || got != -100111 {
		t.Fatalf("target mismatch: got=%T %v", target, target)
	}
	if chatType != "supergroup" {
		t.Fatalf("chat type mismatch: got %q want %q", chatType, "supergroup")
	}
}

func TestResolveTelegramTargetWithChatIDMatchGroupTopic(t *testing.T) {
	contact := contacts.Contact{
		ContactID:       "tg:@alice",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 1001,
		TGGroupChatIDs:  []int64{-100111},
	}
	target, chatType, err := ResolveTelegramTargetWithChatID(contact, "tg:-100111_4425")
	if err != nil {
		t.Fatalf("ResolveTelegramTargetWithChatID() error = %v", err)
	}
	got, ok := target.(telegrambus.DeliveryTarget)
	if !ok || got.ChatID != -100111 || got.MessageThreadID != 4425 {
		t.Fatalf("target mismatch: got=%T %v", target, target)
	}
	if chatType != "supergroup" {
		t.Fatalf("chat type mismatch: got %q want %q", chatType, "supergroup")
	}
}

func TestResolveTelegramTargetWithChatIDFallsBackToPrivate(t *testing.T) {
	contact := contacts.Contact{
		ContactID:       "tg:@alice",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 1001,
		TGGroupChatIDs:  []int64{-100111},
	}
	target, chatType, err := ResolveTelegramTargetWithChatID(contact, "tg:-100222")
	if err != nil {
		t.Fatalf("ResolveTelegramTargetWithChatID() error = %v", err)
	}
	got, ok := target.(int64)
	if !ok || got != 1001 {
		t.Fatalf("target mismatch: got=%T %v", target, target)
	}
	if chatType != "private" {
		t.Fatalf("chat type mismatch: got %q want %q", chatType, "private")
	}
}

func TestResolveTelegramTargetWithChatIDNoPrivateFallback(t *testing.T) {
	contact := contacts.Contact{
		ContactID:      "tg:@alice",
		Kind:           contacts.KindHuman,
		Channel:        contacts.ChannelTelegram,
		TGGroupChatIDs: []int64{-100111},
	}
	target, chatType, err := ResolveTelegramTargetWithChatID(contact, "tg:-100222")
	if err == nil {
		t.Fatalf("ResolveTelegramTargetWithChatID() expected error when no private fallback")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no tg_private_chat_id fallback") {
		t.Fatalf("ResolveTelegramTargetWithChatID() error mismatch: got %q", err.Error())
	}
	if target != nil {
		t.Fatalf("target mismatch: got=%T %v want nil", target, target)
	}
	if chatType != "" {
		t.Fatalf("chatType mismatch: got %q want empty", chatType)
	}
}
