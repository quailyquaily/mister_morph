package slack

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
)

func TestSlackMemorySubjectID(t *testing.T) {
	got := slackMemorySubjectID(slackJob{
		TeamID:    "T123",
		ChannelID: "C456",
	})
	if got != "slack--t123--c456" {
		t.Fatalf("subject_id = %q, want slack--t123--c456", got)
	}
}

func TestSlackMemorySessionID(t *testing.T) {
	job := slackJob{TeamID: "T1", ChannelID: "C1"}
	if got := slackMemorySessionID(job); got != "slack:T1:C1" {
		t.Fatalf("session_id = %q, want slack:T1:C1", got)
	}
	job.ThreadTS = "1739667000.000050"
	if got := slackMemorySessionID(job); got != "slack:T1:C1" {
		t.Fatalf("session_id(thread) = %q, want slack:T1:C1", got)
	}
}

func TestSlackMemoryParticipants(t *testing.T) {
	got := slackMemoryParticipants(slackJob{
		UserID:       "U111",
		MentionUsers: []string{"U222", "U111", " U333 "},
	})
	if len(got) != 3 {
		t.Fatalf("participants len = %d, want 3 (%#v)", len(got), got)
	}
	if got[0].ID != "U111" || got[1].ID != "U222" || got[2].ID != "U333" {
		t.Fatalf("participants order/id mismatch: %#v", got)
	}
	for i, p := range got {
		if p.Protocol != "slack" {
			t.Fatalf("participants[%d].protocol = %q, want slack", i, p.Protocol)
		}
	}
}

func TestSlackMemoryRequestContext(t *testing.T) {
	if got := slackMemoryRequestContext("im"); got != "private" {
		t.Fatalf("im request context = %q, want private", got)
	}
	if got := slackMemoryRequestContext("channel"); got != "public" {
		t.Fatalf("channel request context = %q, want public", got)
	}
}

func TestSlackMemoryCounterpartyLabel(t *testing.T) {
	got := slackMemoryCounterpartyLabel(slackJob{
		UserID:      "U123",
		DisplayName: "Alice",
	})
	if got != "[Alice](slack:U123)" {
		t.Fatalf("counterparty_label = %q, want [Alice](slack:U123)", got)
	}
}

func TestBuildSlackMemoryHistoryCapsLatestItems(t *testing.T) {
	history := []chathistory.ChatHistoryItem{
		{Kind: chathistory.KindInboundUser, Text: "one"},
		{Kind: chathistory.KindInboundUser, Text: "two"},
		{Kind: chathistory.KindInboundUser, Text: "three"},
	}
	got := buildSlackMemoryHistory(history, slackJob{
		ChannelID: "C1",
		ChatType:  "channel",
		UserID:    "U1",
		Username:  "alice",
		Text:      "four",
	}, "five", time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC), 4)
	if len(got) != 4 {
		t.Fatalf("len(history) = %d, want 4", len(got))
	}
	if got[0].Text != "two" || got[1].Text != "three" || got[2].Text != "four" || got[3].Text != "five" {
		t.Fatalf("history texts = %#v, want [two three four five]", []string{got[0].Text, got[1].Text, got[2].Text, got[3].Text})
	}
}
