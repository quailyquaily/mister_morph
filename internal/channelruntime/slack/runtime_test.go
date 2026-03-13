package slack

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestParseSlackInboundEvent_AppMention(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(map[string]any{
		"team_id":    "T111",
		"event_id":   "Ev01",
		"event_time": 1739667600,
		"event": map[string]any{
			"type":         "app_mention",
			"user":         "U111",
			"text":         "<@U999> hello there",
			"channel":      "C222",
			"channel_type": "channel",
			"ts":           "1739667600.000100",
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	event, ok, err := parseSlackInboundEvent(slackSocketEnvelope{
		Type:    "events_api",
		Payload: payload,
	}, "U999")
	if err != nil {
		t.Fatalf("parseSlackInboundEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("parseSlackInboundEvent() ok=false, want true")
	}
	if event.TeamID != "T111" {
		t.Fatalf("team_id mismatch: got %q want %q", event.TeamID, "T111")
	}
	if event.ChannelID != "C222" {
		t.Fatalf("channel_id mismatch: got %q want %q", event.ChannelID, "C222")
	}
	if event.UserID != "U111" {
		t.Fatalf("user_id mismatch: got %q want %q", event.UserID, "U111")
	}
	if !event.IsAppMention {
		t.Fatalf("is_app_mention mismatch: got false want true")
	}
	if event.ChatType != "channel" {
		t.Fatalf("chat_type mismatch: got %q want %q", event.ChatType, "channel")
	}
	if event.SentAt.IsZero() {
		t.Fatalf("sent_at should not be zero")
	}
	wantSentAt := time.Unix(1739667600, 0).UTC()
	if !event.SentAt.Equal(wantSentAt) {
		t.Fatalf("sent_at mismatch: got %s want %s", event.SentAt.Format(time.RFC3339), wantSentAt.Format(time.RFC3339))
	}
}

func TestParseSlackInboundEvent_IgnoresSelfMessage(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(map[string]any{
		"team_id":  "T111",
		"event_id": "Ev02",
		"event": map[string]any{
			"type":    "message",
			"user":    "U999",
			"text":    "hello",
			"channel": "C222",
			"ts":      "1739667600.000100",
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	_, ok, err := parseSlackInboundEvent(slackSocketEnvelope{
		Type:    "events_api",
		Payload: payload,
	}, "U999")
	if err != nil {
		t.Fatalf("parseSlackInboundEvent() error = %v", err)
	}
	if ok {
		t.Fatalf("parseSlackInboundEvent() ok=true, want false")
	}
}

func TestDecideSlackGroupTrigger_Strict(t *testing.T) {
	t.Parallel()

	eventMention := slackInboundEvent{
		Text:            "<@U999> hello",
		IsAppMention:    true,
		IsThreadMessage: false,
	}
	dec, ok, err := decideSlackGroupTrigger(nil, nil, "", eventMention, "U999", "", "strict", 0, 0.6, 0.6, nil, nil)
	if err != nil {
		t.Fatalf("decideSlackGroupTrigger(app_mention) error = %v", err)
	}
	if !ok {
		t.Fatalf("decideSlackGroupTrigger(app_mention) ok=false, want true")
	}
	if dec.Addressing.Impulse != 1 {
		t.Fatalf("addressing_impulse mismatch: got %v want 1", dec.Addressing.Impulse)
	}

	eventIgnored := slackInboundEvent{
		Text: "hello everyone",
	}
	_, ok, err = decideSlackGroupTrigger(nil, nil, "", eventIgnored, "U999", "", "strict", 0, 0.6, 0.6, nil, nil)
	if err != nil {
		t.Fatalf("decideSlackGroupTrigger(non_mention) error = %v", err)
	}
	if ok {
		t.Fatalf("decideSlackGroupTrigger(non_mention) ok=true, want false")
	}

	eventThreadReply := slackInboundEvent{
		Text:            "following up in thread",
		IsThreadMessage: true,
	}
	_, ok, err = decideSlackGroupTrigger(nil, nil, "", eventThreadReply, "U999", "", "strict", 0, 0.6, 0.6, nil, nil)
	if err != nil {
		t.Fatalf("decideSlackGroupTrigger(thread_reply_without_mention) error = %v", err)
	}
	if ok {
		t.Fatalf("decideSlackGroupTrigger(thread_reply_without_mention) ok=true, want false")
	}
}

func TestDecideSlackGroupTrigger_SmartExplicitMentionThreadAndNonThread(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		event slackInboundEvent
	}{
		{
			name: "non_thread_app_mention",
			event: slackInboundEvent{
				Text:            "<@U999> hello",
				IsAppMention:    true,
				IsThreadMessage: false,
				MessageTS:       "1739667600.000100",
			},
		},
		{
			name: "thread_app_mention",
			event: slackInboundEvent{
				Text:            "<@U999> follow up",
				IsAppMention:    true,
				IsThreadMessage: true,
				ThreadTS:        "1739667600.000010",
				MessageTS:       "1739667600.000200",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dec, ok, err := decideSlackGroupTrigger(nil, nil, "", tc.event, "U999", "", "smart", 0, 0.6, 0.6, nil, nil)
			if err != nil {
				t.Fatalf("decideSlackGroupTrigger() error = %v", err)
			}
			if !ok {
				t.Fatalf("decideSlackGroupTrigger() ok=false, want true")
			}
			if dec.Addressing.Impulse != 1 {
				t.Fatalf("addressing_impulse mismatch: got %v want 1", dec.Addressing.Impulse)
			}
		})
	}
}

func TestIntersectSlackCommonReactionEmojiNames(t *testing.T) {
	t.Parallel()

	got := intersectSlackCommonReactionEmojiNames([]string{
		"thumbsup",
		"+1",
		"custom_emoji",
		"WAVE",
		"  chart_with_upwards_trend  ",
		"",
	})
	want := []string{"+1", "wave", "chart_with_upwards_trend"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("intersectSlackCommonReactionEmojiNames() = %#v, want %#v", got, want)
	}
}

func TestIntersectSlackCommonReactionEmojiNames_EmptyInput(t *testing.T) {
	t.Parallel()

	got := intersectSlackCommonReactionEmojiNames(nil)
	if len(got) != 0 {
		t.Fatalf("intersectSlackCommonReactionEmojiNames(nil) len = %d, want 0", len(got))
	}
}
