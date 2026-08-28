package mixin

import (
	"context"
	"testing"

	mixinbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/mixin"
)

func TestMixinExplicitTrigger(t *testing.T) {
	t.Parallel()

	tracker := newRecentMessageTracker(4)
	tracker.Add("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")

	tests := []struct {
		name    string
		inbound mixinbus.InboundMessage
		want    string
		matched bool
	}{
		{
			name: "mention",
			inbound: mixinbus.InboundMessage{
				MentionUserIDs: []string{"33333333-3333-3333-3333-333333333333"},
			},
			want:    "mention",
			matched: true,
		},
		{
			name: "known quote",
			inbound: mixinbus.InboundMessage{
				ConversationID: "11111111-1111-1111-1111-111111111111",
				QuoteMessageID: "22222222-2222-2222-2222-222222222222",
			},
			want:    "reply_to_bot",
			matched: true,
		},
		{name: "bare command is not explicit", inbound: mixinbus.InboundMessage{Text: "/id"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, matched := mixinExplicitTriggerReason(tt.inbound, "33333333-3333-3333-3333-333333333333", tracker)
			if reason != tt.want || matched != tt.matched {
				t.Fatalf("reason, matched = %q, %v; want %q, %v", reason, matched, tt.want, tt.matched)
			}
		})
	}
}

func TestStrictMixinGroupTriggerRejectsBareMessage(t *testing.T) {
	t.Parallel()

	_, accepted, err := decideMixinGroupTrigger(context.Background(), nil, "", mixinbus.InboundMessage{Text: "hello"}, "strict", 0, 0.7, 0.6, nil, "bot", nil)
	if err != nil {
		t.Fatalf("decideMixinGroupTrigger() error = %v", err)
	}
	if accepted {
		t.Fatal("strict mode accepted a bare message")
	}
}
