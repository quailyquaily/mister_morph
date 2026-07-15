package chathistory

import (
	"reflect"
	"testing"
	"time"
)

func TestFilterAfterBoundaryDropsCoveredPrefix(t *testing.T) {
	items := []ChatHistoryItem{
		{Channel: ChannelLark, Kind: KindInboundUser, MessageID: "m1", SentAt: time.Unix(1, 0), Text: "one"},
		{Channel: ChannelLark, Kind: KindOutboundAgent, SentAt: time.Unix(2, 0), Text: "two"},
		{Channel: ChannelLark, Kind: KindInboundUser, MessageID: "m3", SentAt: time.Unix(3, 0), Text: "three"},
	}
	boundary := BoundaryForItem(items[1])
	if boundary == "" {
		t.Fatal("boundary is empty")
	}
	got := FilterAfterBoundary(items, boundary)
	want := items[2:]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered history = %#v, want %#v", got, want)
	}
}

func TestFilterAfterBoundaryUsesStableMessageID(t *testing.T) {
	original := ChatHistoryItem{
		Channel:   ChannelTelegram,
		Kind:      KindInboundUser,
		ChatID:    "1",
		MessageID: "42",
		SentAt:    time.Unix(1, 0),
		Text:      "image",
		Images:    []ChatHistoryImage{{ID: "img_one"}},
	}
	boundary := BoundaryForItem(original)
	mutated := original
	mutated.Images[0].Description = "description added after the run"
	items := []ChatHistoryItem{mutated, {Channel: ChannelTelegram, Kind: KindOutboundAgent, Text: "answer"}}
	got := FilterAfterBoundary(items, boundary)
	if len(got) != 1 || got[0].Text != "answer" {
		t.Fatalf("filtered history = %#v", got)
	}
}

func TestFilterAfterBoundaryKeepsHistoryWhenBoundaryUnknown(t *testing.T) {
	items := []ChatHistoryItem{{Channel: ChannelSlack, Kind: KindInboundUser, MessageID: "new", Text: "new"}}
	got := FilterAfterBoundary(items, "missing-boundary")
	if !reflect.DeepEqual(got, items) {
		t.Fatalf("filtered history = %#v, want unchanged", got)
	}
	if len(got) > 0 {
		got[0].Text = "changed"
		if items[0].Text == "changed" {
			t.Fatal("filter returned alias of input slice")
		}
	}
}
