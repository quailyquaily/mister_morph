package slack

import (
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
)

func TestGenerateSlackPlanProgressMessage(t *testing.T) {
	plan := &agent.Plan{
		Steps: []agent.PlanStep{
			{Step: "scan repo", Status: agent.PlanStatusCompleted},
			{Step: "patch bug", Status: agent.PlanStatusInProgress},
		},
	}
	msg := generateSlackPlanProgressMessage(plan, agent.PlanStepUpdate{
		CompletedIndex: 0,
		CompletedStep:  "scan repo",
		StartedIndex:   1,
		StartedStep:    "patch bug",
	})
	if msg != "scan repo" {
		t.Fatalf("message = %q, want %q", msg, "scan repo")
	}
}

func TestContactsSendRuntimeContextForSlackDirectMessage(t *testing.T) {
	ctx := contactsSendRuntimeContextForSlack(slackJob{
		TeamID:    "T1",
		ChannelID: "D1",
		ChatType:  "im",
		UserID:    "U1",
	})
	if len(ctx.ForbiddenTargetIDs) != 2 {
		t.Fatalf("forbidden_target_ids len = %d, want 2", len(ctx.ForbiddenTargetIDs))
	}
	if ctx.ForbiddenTargetIDs[0] != "slack:T1:U1" {
		t.Fatalf("forbidden_target_ids[0] = %q, want %q", ctx.ForbiddenTargetIDs[0], "slack:T1:U1")
	}
	if ctx.ForbiddenTargetIDs[1] != "slack:T1:D1" {
		t.Fatalf("forbidden_target_ids[1] = %q, want %q", ctx.ForbiddenTargetIDs[1], "slack:T1:D1")
	}
}

func TestNewSlackOutboundReactionHistoryItem(t *testing.T) {
	job := slackJob{
		TeamID:      "T1",
		ChannelID:   "C1",
		ChatType:    "channel",
		ThreadTS:    "1739667600.000100",
		UserID:      "U1",
		Username:    "alice",
		DisplayName: "Alice",
		Text:        "hello",
	}
	item := newSlackOutboundReactionHistoryItem(job, "[reacted: :thumbsup:]", "thumbsup", time.Now().UTC(), "UBOT")
	if item.Kind != chathistory.KindOutboundReaction {
		t.Fatalf("kind = %q, want %q", item.Kind, chathistory.KindOutboundReaction)
	}
	if item.Text != "[reacted: :thumbsup:]" {
		t.Fatalf("text = %q, want %q", item.Text, "[reacted: :thumbsup:]")
	}
}

func TestBuildSlackPromptMessagesSeparatesHistoryAndCurrent(t *testing.T) {
	t.Parallel()

	historyMsg, currentMsg, err := buildSlackPromptMessages([]chathistory.ChatHistoryItem{{
		Channel:   chathistory.ChannelSlack,
		Kind:      chathistory.KindInboundUser,
		MessageID: "101",
		SentAt:    time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC),
		Text:      "earlier",
	}}, slackJob{
		TeamID:      "T1",
		ChannelID:   "C1",
		ChatType:    "channel",
		MessageTS:   "102.0001",
		ThreadTS:    "102.0001",
		UserID:      "U1",
		Username:    "alice",
		DisplayName: "Alice",
		Text:        "latest",
		SentAt:      time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("buildSlackPromptMessages() error = %v", err)
	}
	if historyMsg == nil {
		t.Fatalf("historyMsg = nil")
	}
	if strings.Contains(historyMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("history should not contain latest message: %s", historyMsg.Content)
	}
	if !strings.Contains(historyMsg.Content, "\"text\": \"earlier\"") {
		t.Fatalf("history should contain prior message: %s", historyMsg.Content)
	}
	if currentMsg == nil {
		t.Fatalf("currentMsg = nil")
	}
	if !strings.Contains(currentMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("current message should contain latest text: %s", currentMsg.Content)
	}
}

func TestBuildSlackPromptMessagesOmitsEmptyHistory(t *testing.T) {
	t.Parallel()

	historyMsg, currentMsg, err := buildSlackPromptMessages(nil, slackJob{
		TeamID:      "T1",
		ChannelID:   "C1",
		ChatType:    "channel",
		MessageTS:   "102.0001",
		ThreadTS:    "102.0001",
		UserID:      "U1",
		Username:    "alice",
		DisplayName: "Alice",
		Text:        "latest",
		SentAt:      time.Date(2026, 3, 8, 9, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("buildSlackPromptMessages() error = %v", err)
	}
	if historyMsg != nil {
		t.Fatalf("historyMsg should be nil when history is empty")
	}
	if currentMsg == nil || !strings.Contains(currentMsg.Content, "\"text\": \"latest\"") {
		t.Fatalf("current message should still be present: %#v", currentMsg)
	}
}

func TestBuildSlackHistoryScopeKey(t *testing.T) {
	t.Run("channel scope when thread ts is empty", func(t *testing.T) {
		got, err := buildSlackHistoryScopeKey("T1", "C1", "")
		if err != nil {
			t.Fatalf("buildSlackHistoryScopeKey() error = %v", err)
		}
		if got != "slack:T1:C1" {
			t.Fatalf("history scope key = %q, want %q", got, "slack:T1:C1")
		}
	})

	t.Run("thread scope when thread ts exists", func(t *testing.T) {
		got, err := buildSlackHistoryScopeKey("T1", "C1", "1739667600.000100")
		if err != nil {
			t.Fatalf("buildSlackHistoryScopeKey() error = %v", err)
		}
		if got != "slack:T1:C1:thread:1739667600.000100" {
			t.Fatalf("history scope key = %q, want %q", got, "slack:T1:C1:thread:1739667600.000100")
		}
	})
}

func TestSlackHistoryScopeKeyForJob(t *testing.T) {
	if got := slackHistoryScopeKeyForJob(slackJob{
		TeamID:          "T1",
		ChannelID:       "C1",
		ThreadTS:        "1739667600.000100",
		ConversationKey: "slack:T1:C1",
	}); got != "slack:T1:C1:thread:1739667600.000100" {
		t.Fatalf("scope = %q, want thread scope key", got)
	}
	if got := slackHistoryScopeKeyForJob(slackJob{
		TeamID:          "T1",
		ChannelID:       "C1",
		MessageTS:       "1739667600.000100",
		ThreadTS:        "1739667600.000100",
		ConversationKey: "slack:T1:C1",
	}); got != "slack:T1:C1" {
		t.Fatalf("scope = %q, want channel scope key for synthetic thread", got)
	}
	if got := slackHistoryScopeKeyForJob(slackJob{
		ConversationKey: "slack:T1:C1",
	}); got != "slack:T1:C1" {
		t.Fatalf("scope = %q, want conversation key fallback", got)
	}
}

func TestSlackHistoryScopeBehavior_DifferentThreadsIsolated(t *testing.T) {
	history := map[string][]string{}
	appendByJob := func(job slackJob, text string) {
		scope := slackHistoryScopeKeyForJob(job)
		history[scope] = append(history[scope], text)
	}

	scopeA, err := buildSlackHistoryScopeKey("T1", "C1", "1739667600.000100")
	if err != nil {
		t.Fatalf("buildSlackHistoryScopeKey(scopeA) error = %v", err)
	}
	scopeB, err := buildSlackHistoryScopeKey("T1", "C1", "1739667600.000200")
	if err != nil {
		t.Fatalf("buildSlackHistoryScopeKey(scopeB) error = %v", err)
	}
	if scopeA == scopeB {
		t.Fatalf("thread scope keys should differ: %q", scopeA)
	}

	appendByJob(slackJob{ConversationKey: "slack:T1:C1", TeamID: "T1", ChannelID: "C1", ThreadTS: "1739667600.000100"}, "thread-a-1")
	appendByJob(slackJob{ConversationKey: "slack:T1:C1", TeamID: "T1", ChannelID: "C1", ThreadTS: "1739667600.000200"}, "thread-b-1")
	appendByJob(slackJob{ConversationKey: "slack:T1:C1", TeamID: "T1", ChannelID: "C1", ThreadTS: "1739667600.000100"}, "thread-a-2")

	if got := history[scopeA]; len(got) != 2 || got[0] != "thread-a-1" || got[1] != "thread-a-2" {
		t.Fatalf("scopeA history = %#v, want [thread-a-1 thread-a-2]", got)
	}
	if got := history[scopeB]; len(got) != 1 || got[0] != "thread-b-1" {
		t.Fatalf("scopeB history = %#v, want [thread-b-1]", got)
	}
}

func TestSlackHistoryScopeBehavior_SameThreadShared(t *testing.T) {
	history := map[string][]string{}
	appendByJob := func(job slackJob, text string) {
		scope := slackHistoryScopeKeyForJob(job)
		history[scope] = append(history[scope], text)
	}

	scope, err := buildSlackHistoryScopeKey("T1", "C1", "1739667600.000100")
	if err != nil {
		t.Fatalf("buildSlackHistoryScopeKey() error = %v", err)
	}
	appendByJob(slackJob{ConversationKey: "slack:T1:C1", TeamID: "T1", ChannelID: "C1", ThreadTS: "1739667600.000100"}, "m1")
	appendByJob(slackJob{ConversationKey: "slack:T1:C1", TeamID: "T1", ChannelID: "C1", ThreadTS: "1739667600.000100"}, "m2")

	if got := history[scope]; len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("scope history = %#v, want [m1 m2]", got)
	}
}

func TestSlackHistoryScopeBehavior_NoThreadUsesChannelScope(t *testing.T) {
	history := map[string][]string{}
	appendByJob := func(job slackJob, text string) {
		scope := slackHistoryScopeKeyForJob(job)
		history[scope] = append(history[scope], text)
	}

	channelScope, err := buildSlackHistoryScopeKey("T1", "C1", "")
	if err != nil {
		t.Fatalf("buildSlackHistoryScopeKey() error = %v", err)
	}
	if channelScope != "slack:T1:C1" {
		t.Fatalf("channel scope = %q, want slack:T1:C1", channelScope)
	}

	appendByJob(slackJob{ConversationKey: "slack:T1:C1"}, "m1")
	appendByJob(slackJob{ConversationKey: "slack:T1:C1"}, "m2")
	if got := history[channelScope]; len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("channel history = %#v, want [m1 m2]", got)
	}
}
