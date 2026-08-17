package telegram

import (
	"context"
	"fmt"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

type stubAddressingLLMClient struct {
	results []llm.Result
	err     error
	calls   []llm.Request
}

func (s *stubAddressingLLMClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return llm.Result{}, s.err
	}
	if len(s.results) == 0 {
		return llm.Result{}, fmt.Errorf("no stub result")
	}
	res := s.results[0]
	s.results = s.results[1:]
	return res, nil
}

type stubAddressingTool struct {
	name        string
	execCount   int
	lastEmoji   string
	failOnEmoji string
}

func (s *stubAddressingTool) Name() string { return s.name }

func (s *stubAddressingTool) Description() string { return "stub tool" }

func (s *stubAddressingTool) ParameterSchema() string {
	return `{"type":"object","properties":{"emoji":{"type":"string"}},"required":["emoji"]}`
}

func (s *stubAddressingTool) Execute(_ context.Context, params map[string]any) (string, error) {
	s.execCount++
	emoji, _ := params["emoji"].(string)
	s.lastEmoji = emoji
	if emoji == s.failOnEmoji {
		return "", fmt.Errorf("emoji not allowed: %s", emoji)
	}
	return "ok", nil
}

func TestAddressingDecisionViaLLM_EnforceLightweightReaction(t *testing.T) {
	client := &stubAddressingLLMClient{
		results: []llm.Result{
			{Text: `{"addressed":false,"confidence":0.2,"wanna_interject":true,"interject":0.1,"impulse":0.3,"is_lightweight":true,"reaction":"🤨","reason":"x"}`},
		},
	}
	tool := &stubAddressingTool{name: "message_react"}

	got, ok, err := addressingDecisionViaLLM(context.Background(), client, "gpt-5.2", nil, "啧", nil, tool)
	if err != nil {
		t.Fatalf("addressingDecisionViaLLM() error = %v", err)
	}
	if !ok {
		t.Fatalf("addressingDecisionViaLLM() ok = false, want true")
	}
	if !got.IsLightweight {
		t.Fatalf("IsLightweight = false, want true")
	}
	if tool.execCount != 1 {
		t.Fatalf("tool exec count = %d, want 1", tool.execCount)
	}
	if tool.lastEmoji != "🤨" {
		t.Fatalf("last emoji = %q, want %q", tool.lastEmoji, "🤨")
	}
}

func TestShouldSkipGroupReplyWithoutBodyMention_IgnoresForumTopicRootReply(t *testing.T) {
	msg := &telegramMessage{
		Text: "hi",
		From: &telegramUser{ID: 10},
		ReplyTo: &telegramMessage{
			MessageID:         246,
			MessageThreadID:   246,
			IsTopicMessage:    true,
			ForumTopicCreated: []byte(`{"name":"topic"}`),
			From:              &telegramUser{ID: 20, Username: "topic_creator"},
		},
	}

	if shouldSkipGroupReplyWithoutBodyMention(msg, "hi", "morph_bot", 99) {
		t.Fatalf("topic root reply should not be treated as replying to another user")
	}
}

func TestShouldSkipGroupReplyWithoutBodyMention_SkipsHumanReplyWithoutMention(t *testing.T) {
	msg := &telegramMessage{
		Text:    "hi",
		From:    &telegramUser{ID: 10},
		ReplyTo: &telegramMessage{MessageID: 42, From: &telegramUser{ID: 20, Username: "alice"}},
	}

	if !shouldSkipGroupReplyWithoutBodyMention(msg, "hi", "morph_bot", 99) {
		t.Fatalf("plain human reply without bot mention should be skipped")
	}
}

func TestGroupExplicitMentionReason_IgnoresForumTopicRootReplyFromBot(t *testing.T) {
	msg := &telegramMessage{
		Text: "hi",
		ReplyTo: &telegramMessage{
			MessageID:         246,
			MessageThreadID:   246,
			IsTopicMessage:    true,
			ForumTopicCreated: []byte(`{"name":"topic"}`),
			From:              &telegramUser{ID: 99, IsBot: true, Username: "morph_bot"},
		},
	}

	if reason, ok := groupExplicitMentionReason(msg, "hi", "morph_bot", 99); ok {
		t.Fatalf("topic root reply should not count as explicit bot reply: reason=%q", reason)
	}
}

func TestCollectMentionCandidates_IgnoresForumTopicRootReplySender(t *testing.T) {
	msg := &telegramMessage{
		Text: "hi",
		From: &telegramUser{ID: 10, Username: "sender"},
		ReplyTo: &telegramMessage{
			MessageID:         246,
			MessageThreadID:   246,
			IsTopicMessage:    true,
			ForumTopicCreated: []byte(`{"name":"topic"}`),
			From:              &telegramUser{ID: 20, Username: "topic_creator"},
		},
	}

	got := collectMentionCandidates(msg, "morph_bot")
	if len(got) != 1 || got[0] != "@sender" {
		t.Fatalf("mention candidates = %#v, want [@sender]", got)
	}
}

func TestTelegramFirstBodyMentionTargetsSelf(t *testing.T) {
	tests := []struct {
		name       string
		message    *telegramMessage
		wantFound  bool
		wantTarget bool
	}{
		{
			name: "username mention targets self",
			message: &telegramMessage{
				Text:     "@morph_bot please continue",
				Entities: []telegramEntity{{Type: "mention", Offset: 0, Length: 10}},
			},
			wantFound:  true,
			wantTarget: true,
		},
		{
			name: "first mention targets another agent",
			message: &telegramMessage{
				Text: "@smith_bot then @morph_bot",
				Entities: []telegramEntity{
					{Type: "mention", Offset: 16, Length: 10},
					{Type: "mention", Offset: 0, Length: 10},
				},
			},
			wantFound: true,
		},
		{
			name: "text mention user id targets self",
			message: &telegramMessage{
				Text:     "Morph please continue",
				Entities: []telegramEntity{{Type: "text_mention", Offset: 0, Length: 5, User: &telegramUser{ID: 99}}},
			},
			wantFound:  true,
			wantTarget: true,
		},
		{
			name: "caption mention",
			message: &telegramMessage{
				Caption:         "@morph_bot inspect this",
				CaptionEntities: []telegramEntity{{Type: "mention", Offset: 0, Length: 10}},
			},
			wantFound:  true,
			wantTarget: true,
		},
		{name: "no mention", message: &telegramMessage{Text: "plain task"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, targetsSelf := telegramFirstBodyMentionTargetsSelf(tt.message, "morph_bot", 99)
			if found != tt.wantFound || targetsSelf != tt.wantTarget {
				t.Fatalf("telegramFirstBodyMentionTargetsSelf() = (%v, %v), want (%v, %v)", found, targetsSelf, tt.wantFound, tt.wantTarget)
			}
		})
	}
}

func TestShouldIgnoreTelegramFirstMention(t *testing.T) {
	tests := []struct {
		name        string
		isGroup     bool
		fromAgent   bool
		found       bool
		targetsSelf bool
		want        bool
	}{
		{name: "private agent without mention", fromAgent: true},
		{name: "private agent mentioning another agent", fromAgent: true, found: true},
		{name: "group agent without mention", isGroup: true, fromAgent: true, want: true},
		{name: "group agent mentioning self", isGroup: true, fromAgent: true, found: true, targetsSelf: true},
		{name: "group agent mentioning another agent", isGroup: true, fromAgent: true, found: true, want: true},
		{name: "group human without mention", isGroup: true},
		{name: "group human mentioning another agent", isGroup: true, found: true, want: true},
		{name: "private human mentioning another agent", found: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldIgnoreTelegramFirstMention(tt.isGroup, tt.fromAgent, tt.found, tt.targetsSelf)
			if got != tt.want {
				t.Fatalf("shouldIgnoreTelegramFirstMention() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddressingDecisionViaLLM_EnforceLightweightReactionReturnsErrorOnToolFailure(t *testing.T) {
	client := &stubAddressingLLMClient{
		results: []llm.Result{
			{Text: `{"addressed":false,"confidence":0.2,"wanna_interject":true,"interject":0.1,"impulse":0.3,"is_lightweight":true,"reaction":"🚫","reason":"x"}`},
		},
	}
	tool := &stubAddressingTool{name: "message_react", failOnEmoji: "🚫"}

	_, _, err := addressingDecisionViaLLM(context.Background(), client, "gpt-5.2", nil, "啧", nil, tool)
	if err == nil {
		t.Fatalf("addressingDecisionViaLLM() error = nil, want non-nil")
	}
	if tool.execCount != 1 {
		t.Fatalf("tool exec count = %d, want 1", tool.execCount)
	}
}

func TestAddressingDecisionViaLLM_EmptyReactionKeepsLightweight(t *testing.T) {
	client := &stubAddressingLLMClient{
		results: []llm.Result{
			{Text: `{"addressed":false,"confidence":0.2,"wanna_interject":true,"interject":0.1,"impulse":0.3,"is_lightweight":true,"reaction":"","reason":"x"}`},
		},
	}
	tool := &stubAddressingTool{name: "message_react"}

	got, ok, err := addressingDecisionViaLLM(context.Background(), client, "gpt-5.2", nil, "啧", nil, tool)
	if err != nil {
		t.Fatalf("addressingDecisionViaLLM() error = %v", err)
	}
	if !ok {
		t.Fatalf("addressingDecisionViaLLM() ok = false, want true")
	}
	if !got.IsLightweight {
		t.Fatalf("IsLightweight = false, want true")
	}
	if tool.execCount != 0 {
		t.Fatalf("tool exec count = %d, want 0", tool.execCount)
	}
}

func TestAddressingDecisionViaLLM_NoReactionWhenNotLightweight(t *testing.T) {
	client := &stubAddressingLLMClient{
		results: []llm.Result{
			{Text: `{"addressed":false,"confidence":0.2,"wanna_interject":false,"interject":0.05,"impulse":0.1,"is_lightweight":false,"reason":"x"}`},
		},
	}
	tool := &stubAddressingTool{name: "message_react"}

	got, ok, err := addressingDecisionViaLLM(context.Background(), client, "gpt-5.2", nil, "啧", nil, tool)
	if err != nil {
		t.Fatalf("addressingDecisionViaLLM() error = %v", err)
	}
	if !ok {
		t.Fatalf("addressingDecisionViaLLM() ok = false, want true")
	}
	if got.IsLightweight {
		t.Fatalf("IsLightweight = true, want false")
	}
	if tool.execCount != 0 {
		t.Fatalf("tool exec count = %d, want 0", tool.execCount)
	}
}

func TestAddressingDecisionViaLLM_NoDuplicateReactionWhenModelAlreadyCalledTool(t *testing.T) {
	client := &stubAddressingLLMClient{
		results: []llm.Result{
			{
				ToolCalls: []llm.ToolCall{
					{
						ID:        "tc_1",
						Name:      "message_react",
						Arguments: map[string]any{"emoji": "🤨"},
					},
				},
			},
			{Text: `{"addressed":false,"confidence":0.2,"wanna_interject":true,"interject":0.1,"impulse":0.3,"is_lightweight":true,"reaction":"🤨","reason":"x"}`},
		},
	}
	tool := &stubAddressingTool{name: "message_react"}

	got, ok, err := addressingDecisionViaLLM(context.Background(), client, "gpt-5.2", nil, "啧", nil, tool)
	if err != nil {
		t.Fatalf("addressingDecisionViaLLM() error = %v", err)
	}
	if !ok {
		t.Fatalf("addressingDecisionViaLLM() ok = false, want true")
	}
	if !got.IsLightweight {
		t.Fatalf("IsLightweight = false, want true")
	}
	if tool.execCount != 1 {
		t.Fatalf("tool exec count = %d, want 1", tool.execCount)
	}
}
