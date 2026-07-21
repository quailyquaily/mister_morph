package line

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	linebus "github.com/quailyquaily/mistermorph/internal/bus/adapters/line"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/grouptrigger"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/llm"
)

type lineGroupTriggerDecision = grouptrigger.Decision

func decideLineGroupTrigger(
	ctx context.Context,
	client llm.Client,
	model string,
	inbound linebus.InboundMessage,
	botUserID string,
	mode string,
	addressingLLMTimeout time.Duration,
	addressingConfidenceThreshold float64,
	addressingInterjectThreshold float64,
	history []chathistory.ChatHistoryItem,
	personaDir ...string,
) (lineGroupTriggerDecision, bool, error) {
	explicitReason, explicitMatched := lineExplicitTriggerReason(inbound, botUserID)
	return grouptrigger.Decide(ctx, grouptrigger.DecideOptions{
		Mode:                     mode,
		ConfidenceThreshold:      addressingConfidenceThreshold,
		InterjectThreshold:       addressingInterjectThreshold,
		ExplicitReason:           explicitReason,
		ExplicitMatched:          explicitMatched,
		AddressingFallbackReason: mode,
		AddressingTimeout:        addressingLLMTimeout,
		Addressing: func(addrCtx context.Context) (grouptrigger.Addressing, bool, error) {
			return lineAddressingDecisionViaLLM(addrCtx, client, model, inbound, history, personaDir...)
		},
	})
}

func lineExplicitTriggerReason(inbound linebus.InboundMessage, botUserID string) (string, bool) {
	if lineMessageMentionsBot(inbound, botUserID) {
		return "mention", true
	}
	if lineCommandTriggered(inbound.Text) {
		return "command_prefix", true
	}
	return "", false
}

func lineMessageMentionsBot(inbound linebus.InboundMessage, botUserID string) bool {
	botUserID = strings.TrimSpace(botUserID)
	if botUserID == "" {
		return false
	}
	for _, raw := range inbound.MentionUsers {
		if strings.TrimSpace(raw) == botUserID {
			return true
		}
	}
	return false
}

func lineCommandTriggered(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "/")
}

func lineAddressingDecisionViaLLM(
	ctx context.Context,
	client llm.Client,
	model string,
	inbound linebus.InboundMessage,
	history []chathistory.ChatHistoryItem,
	personaDir ...string,
) (grouptrigger.Addressing, bool, error) {
	if ctx == nil || client == nil {
		return grouptrigger.Addressing{}, false, nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return grouptrigger.Addressing{}, false, fmt.Errorf("missing model for addressing_llm")
	}
	historyMessages := chathistory.BuildMessages(chathistory.ChannelLine, history)
	currentMessage := map[string]any{
		"chat_id":       strings.TrimSpace(inbound.ChatID),
		"chat_type":     strings.TrimSpace(inbound.ChatType),
		"message_id":    strings.TrimSpace(inbound.MessageID),
		"event_id":      strings.TrimSpace(inbound.EventID),
		"from_user_id":  strings.TrimSpace(inbound.FromUserID),
		"text":          strings.TrimSpace(inbound.Text),
		"mention_users": append([]string(nil), inbound.MentionUsers...),
	}
	systemPrompt, userPrompt, err := grouptrigger.RenderAddressingPrompts(loadLineAddressingPersonaIdentity(personaDir...), "", currentMessage, historyMessages)
	if err != nil {
		return grouptrigger.Addressing{}, false, fmt.Errorf("render addressing prompts: %w", err)
	}
	return grouptrigger.DecideViaLLM(ctx, grouptrigger.LLMDecisionOptions{
		Client:       client,
		Model:        model,
		Scene:        "line.addressing_decision",
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
	})
}

func loadLineAddressingPersonaIdentity(personaDir ...string) string {
	spec := agent.PromptSpec{}
	promptprofile.ApplyPersonaIdentity(&spec, slog.Default(), personaDir...)
	return strings.TrimSpace(spec.Identity)
}
