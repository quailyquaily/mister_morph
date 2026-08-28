package mixin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	mixinbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/mixin"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/grouptrigger"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/llm"
)

type recentMessageTracker struct {
	mu       sync.Mutex
	limit    int
	messages map[string][]string
}

func newRecentMessageTracker(limit int) *recentMessageTracker {
	if limit <= 0 {
		limit = 256
	}
	return &recentMessageTracker{limit: limit, messages: make(map[string][]string)}
}

func (t *recentMessageTracker) Add(conversationID, messageID string) {
	if t == nil || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(messageID) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	items := append(t.messages[conversationID], messageID)
	if len(items) > t.limit {
		items = append([]string(nil), items[len(items)-t.limit:]...)
	}
	t.messages[conversationID] = items
}

func (t *recentMessageTracker) Contains(conversationID, messageID string) bool {
	if t == nil || strings.TrimSpace(messageID) == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, item := range t.messages[conversationID] {
		if item == messageID {
			return true
		}
	}
	return false
}

func mixinExplicitTriggerReason(inbound mixinbus.InboundMessage, botUserID string, recent *recentMessageTracker) (string, bool) {
	for _, userID := range inbound.MentionUserIDs {
		if strings.EqualFold(strings.TrimSpace(userID), strings.TrimSpace(botUserID)) {
			return "mention", true
		}
	}
	if recent.Contains(inbound.ConversationID, inbound.QuoteMessageID) {
		return "reply_to_bot", true
	}
	return "", false
}

func decideMixinGroupTrigger(
	ctx context.Context,
	client llm.Client,
	model string,
	inbound mixinbus.InboundMessage,
	mode string,
	addressingTimeout time.Duration,
	confidenceThreshold float64,
	interjectThreshold float64,
	history []chathistory.ChatHistoryItem,
	botUserID string,
	recent *recentMessageTracker,
	personaDir ...string,
) (grouptrigger.Decision, bool, error) {
	reason, explicit := mixinExplicitTriggerReason(inbound, botUserID, recent)
	return grouptrigger.Decide(ctx, grouptrigger.DecideOptions{
		Mode:                     mode,
		ConfidenceThreshold:      confidenceThreshold,
		InterjectThreshold:       interjectThreshold,
		ExplicitReason:           reason,
		ExplicitMatched:          explicit,
		AddressingFallbackReason: mode,
		AddressingTimeout:        addressingTimeout,
		Addressing: func(addrCtx context.Context) (grouptrigger.Addressing, bool, error) {
			return mixinAddressingDecisionViaLLM(addrCtx, client, model, inbound, history, personaDir...)
		},
	})
}

func mixinAddressingDecisionViaLLM(ctx context.Context, client llm.Client, model string, inbound mixinbus.InboundMessage, history []chathistory.ChatHistoryItem, personaDir ...string) (grouptrigger.Addressing, bool, error) {
	if ctx == nil || client == nil {
		return grouptrigger.Addressing{}, false, nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return grouptrigger.Addressing{}, false, fmt.Errorf("missing model for addressing_llm")
	}
	current := map[string]any{
		"conversation_id":  inbound.ConversationID,
		"chat_type":        inbound.ChatType,
		"message_id":       inbound.MessageID,
		"from_user_id":     inbound.FromUserID,
		"mixin_id":         inbound.IdentityNumber,
		"display_name":     inbound.DisplayName,
		"text":             inbound.Text,
		"quote_message_id": inbound.QuoteMessageID,
	}
	spec := agent.PromptSpec{}
	promptprofile.ApplyPersonaIdentity(&spec, slog.Default(), personaDir...)
	systemPrompt, userPrompt, err := grouptrigger.RenderAddressingPrompts(strings.TrimSpace(spec.Identity), "", current, chathistory.BuildMessages(chathistory.ChannelMixin, history))
	if err != nil {
		return grouptrigger.Addressing{}, false, fmt.Errorf("render addressing prompts: %w", err)
	}
	return grouptrigger.DecideViaLLM(ctx, grouptrigger.LLMDecisionOptions{
		Client: client, Model: model, Scene: "mixin.addressing_decision", SystemPrompt: systemPrompt, UserPrompt: userPrompt,
	})
}
