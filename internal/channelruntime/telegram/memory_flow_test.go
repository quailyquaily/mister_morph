package telegram

import (
	"testing"

	"github.com/quailyquaily/mistermorph/memory"
)

func TestBuildMemoryWriteMeta(t *testing.T) {
	t.Run("normal chat keeps tg session and contact meta", func(t *testing.T) {
		meta := buildMemoryWriteMeta(telegramJob{
			ChatID:          777,
			FromUserID:      1001,
			FromUsername:    "@alice",
			FromDisplayName: "Alice",
		})
		if meta.SessionID != "tg:777" {
			t.Fatalf("session_id = %q, want %q", meta.SessionID, "tg:777")
		}
		if len(meta.ContactIDs) != 1 || meta.ContactIDs[0] != "tg:@alice" {
			t.Fatalf("contact_ids = %#v, want [\"tg:@alice\"]", meta.ContactIDs)
		}
		if len(meta.ContactNicknames) != 1 || meta.ContactNicknames[0] != "Alice" {
			t.Fatalf("contact_nicknames = %#v, want [\"Alice\"]", meta.ContactNicknames)
		}
	})
}

func TestTelegramMemorySessionID(t *testing.T) {
	job := telegramJob{ChatID: -1001234567890}
	if got := telegramMemorySessionID(job); got != "tg:-1001234567890" {
		t.Fatalf("session_id = %q, want %q", got, "tg:-1001234567890")
	}
}

func TestTelegramMemorySessionIDIncludesTopic(t *testing.T) {
	job := telegramJob{ChatID: -1001234567890, MessageThreadID: 4425}
	if got := telegramMemorySessionID(job); got != "tg:-1001234567890_4425" {
		t.Fatalf("session_id = %q, want %q", got, "tg:-1001234567890_4425")
	}
}

func TestTelegramMemoryRequestContext(t *testing.T) {
	if got := telegramMemoryRequestContext("private"); got != memory.ContextPrivate {
		t.Fatalf("private context = %q, want %q", got, memory.ContextPrivate)
	}
	if got := telegramMemoryRequestContext("supergroup"); got != memory.ContextPublic {
		t.Fatalf("group context = %q, want %q", got, memory.ContextPublic)
	}
}

func TestTelegramMemoryParticipantsIncludeSenderAndMentions(t *testing.T) {
	job := telegramJob{
		FromUsername:    "johnwick",
		FromDisplayName: "John Wick",
		MentionUsers:    []string{"alice", "@bob", "alice", "   "},
	}
	got := telegramMemoryParticipants(job)
	if len(got) != 3 {
		t.Fatalf("participants len = %d, want 3 (%#v)", len(got), got)
	}
	if got[0].ID != "@johnwick" || got[0].Nickname != "John Wick" || got[0].Protocol != "tg" {
		t.Fatalf("sender participant = %#v, want id=@johnwick nickname=John Wick protocol=tg", got[0])
	}
	if got[1].ID != "@alice" || got[1].Nickname != "@alice" || got[1].Protocol != "tg" {
		t.Fatalf("mention participant 1 = %#v, want id=@alice nickname=@alice protocol=tg", got[1])
	}
	if got[2].ID != "@bob" || got[2].Nickname != "@bob" || got[2].Protocol != "tg" {
		t.Fatalf("mention participant 2 = %#v, want id=@bob nickname=@bob protocol=tg", got[2])
	}
}

func TestTelegramMemoryParticipantsFallbackToNumericSenderID(t *testing.T) {
	job := telegramJob{
		FromUserID:      28036192,
		FromDisplayName: "Lyric",
	}
	got := telegramMemoryParticipants(job)
	if len(got) != 1 {
		t.Fatalf("participants len = %d, want 1 (%#v)", len(got), got)
	}
	if got[0].ID != "28036192" || got[0].Nickname != "Lyric" || got[0].Protocol != "tg" {
		t.Fatalf("sender participant = %#v, want id=28036192 nickname=Lyric protocol=tg", got[0])
	}
}

func TestBuildMemoryCounterpartyLabel(t *testing.T) {
	t.Run("formats markdown ref from contact meta", func(t *testing.T) {
		got := buildMemoryCounterpartyLabel(memory.WriteMeta{
			ContactIDs:       []string{"tg:@alice"},
			ContactNicknames: []string{"Alice"},
		}, memory.SessionContext{})
		if got != "[Alice](tg:@alice)" {
			t.Fatalf("counterparty_label = %q, want %q", got, "[Alice](tg:@alice)")
		}
	})

	t.Run("falls back to handle in display ref style", func(t *testing.T) {
		got := buildMemoryCounterpartyLabel(memory.WriteMeta{}, memory.SessionContext{
			CounterpartyHandle: "@alice",
		})
		if got != "[alice](tg:@alice)" {
			t.Fatalf("counterparty_label = %q, want %q", got, "[alice](tg:@alice)")
		}
	})

	t.Run("keeps plain nickname when no reference id exists", func(t *testing.T) {
		got := buildMemoryCounterpartyLabel(memory.WriteMeta{}, memory.SessionContext{
			CounterpartyName: "Alice",
		})
		if got != "Alice" {
			t.Fatalf("counterparty_label = %q, want %q", got, "Alice")
		}
	})
}
