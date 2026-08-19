package telegram

import (
	"testing"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	telegrambus "github.com/quailyquaily/mistermorph/internal/bus/adapters/telegram"
)

func TestTelegramTaskConversationUsesTopicAndNormalizedUsernames(t *testing.T) {
	got := telegramTaskConversation("tg:-1001_42", telegrambus.InboundMessage{
		ChatType:        "supergroup",
		FromUserID:      1001,
		FromUsername:    "alice",
		FromDisplayName: "Alice",
		MentionUsers:    []string{"morphbot", "bob", "@carol", "bob"},
	}, "morphbot", 999)
	if got == nil || got.ConversationID != "tg:-1001_42" || got.ConversationType != "supergroup" {
		t.Fatalf("conversation = %+v", got)
	}
	if len(got.Participants) != 3 {
		t.Fatalf("participants = %+v, want 3", got.Participants)
	}
	if got.Participants[0].ID != "@alice" || got.Participants[0].Nickname != "Alice" {
		t.Fatalf("sender = %+v", got.Participants[0])
	}
	if got.Participants[1].ID != "@bob" || got.Participants[2].ID != "@carol" {
		t.Fatalf("mentions = %+v", got.Participants[1:])
	}
	if got.Participants[1].Nickname != "" || got.Participants[2].Nickname != "" {
		t.Fatalf("mention nicknames = %+v, want empty", got.Participants[1:])
	}
}

func TestTelegramTaskConversationIncludesParticipantSnapshotWithoutUsername(t *testing.T) {
	got := telegramTaskConversation("tg:-1001", telegrambus.InboundMessage{
		ChatType:        "supergroup",
		FromUserID:      1001,
		FromUsername:    "alice",
		FromDisplayName: "Alice",
		MentionUsers:    []string{"bob"},
		MentionParticipants: []busruntime.MessageParticipant{
			{ID: "42", Nickname: "No Handle"},
			{ID: "@Bob", Nickname: "Bob"},
		},
	}, "morphbot", 999)
	if got == nil {
		t.Fatal("conversation = nil")
	}
	if len(got.Participants) != 3 {
		t.Fatalf("participants = %+v, want sender and two mentions", got.Participants)
	}
	if got.Participants[1].ID != "42" || got.Participants[1].Nickname != "No Handle" {
		t.Fatalf("participant without username = %+v", got.Participants[1])
	}
	if got.Participants[2].ID != "@Bob" || got.Participants[2].Nickname != "Bob" {
		t.Fatalf("participant with username = %+v", got.Participants[2])
	}
}

func TestCollectTelegramTaskParticipantsUsesAvailableUserIdentity(t *testing.T) {
	msg := &telegramMessage{
		Text: "No Handle, Bob, and Morph",
		ReplyTo: &telegramMessage{From: &telegramUser{
			ID:        41,
			FirstName: "Reply",
			LastName:  "User",
		}},
		Entities: []telegramEntity{
			{Type: "text_mention", User: &telegramUser{ID: 42, FirstName: "No", LastName: "Handle"}},
			{Type: "text_mention", User: &telegramUser{ID: 43, Username: "bob", FirstName: "Bob"}},
			{Type: "text_mention", User: &telegramUser{ID: 999, Username: "morphbot", FirstName: "Morph"}},
		},
	}

	got := collectTelegramTaskParticipants(msg, "morphbot", 999)
	want := []busruntime.MessageParticipant{
		{ID: "41", Nickname: "Reply User"},
		{ID: "42", Nickname: "No Handle"},
		{ID: "@bob", Nickname: "Bob"},
	}
	if len(got) != len(want) {
		t.Fatalf("participants = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("participants[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
