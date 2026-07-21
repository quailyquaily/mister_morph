package lark

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/grouptrigger"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/llm"
)

type larkGroupTriggerDecision = grouptrigger.Decision

func decideLarkGroupTrigger(
	ctx context.Context,
	client llm.Client,
	model string,
	inbound larkbus.InboundMessage,
	mode string,
	addressingLLMTimeout time.Duration,
	addressingConfidenceThreshold float64,
	addressingInterjectThreshold float64,
	history []chathistory.ChatHistoryItem,
	personaDir ...string,
) (larkGroupTriggerDecision, bool, error) {
	explicitReason, explicitMatched := larkExplicitTriggerReason(inbound)
	return grouptrigger.Decide(ctx, grouptrigger.DecideOptions{
		Mode:                     mode,
		ConfidenceThreshold:      addressingConfidenceThreshold,
		InterjectThreshold:       addressingInterjectThreshold,
		ExplicitReason:           explicitReason,
		ExplicitMatched:          explicitMatched,
		AddressingFallbackReason: mode,
		AddressingTimeout:        addressingLLMTimeout,
		Addressing: func(addrCtx context.Context) (grouptrigger.Addressing, bool, error) {
			return larkAddressingDecisionViaLLM(addrCtx, client, model, inbound, history, personaDir...)
		},
	})
}

func larkExplicitTriggerReason(inbound larkbus.InboundMessage) (string, bool) {
	if larkMessageLooksMentioned(inbound) {
		return "mention", true
	}
	if larkCommandTriggered(inbound.Text) {
		return "command_prefix", true
	}
	return "", false
}

func larkMessageLooksMentioned(inbound larkbus.InboundMessage) bool {
	return len(inbound.MentionUsers) > 0
}

func larkCommandTriggered(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "/")
}

func larkAddressingDecisionViaLLM(
	ctx context.Context,
	client llm.Client,
	model string,
	inbound larkbus.InboundMessage,
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
	historyMessages := chathistory.BuildMessages(chathistory.ChannelLark, history)
	currentMessage := map[string]any{
		"chat_id":       strings.TrimSpace(inbound.ChatID),
		"chat_type":     strings.TrimSpace(inbound.ChatType),
		"message_id":    strings.TrimSpace(inbound.MessageID),
		"event_id":      strings.TrimSpace(inbound.EventID),
		"from_open_id":  strings.TrimSpace(inbound.FromUserID),
		"text":          strings.TrimSpace(inbound.Text),
		"mention_users": append([]string(nil), inbound.MentionUsers...),
	}
	systemPrompt, userPrompt, err := grouptrigger.RenderAddressingPrompts(loadLarkAddressingPersonaIdentity(personaDir...), "", currentMessage, historyMessages)
	if err != nil {
		return grouptrigger.Addressing{}, false, fmt.Errorf("render addressing prompts: %w", err)
	}
	return grouptrigger.DecideViaLLM(ctx, grouptrigger.LLMDecisionOptions{
		Client:       client,
		Model:        model,
		Scene:        "lark.addressing_decision",
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
	})
}

func loadLarkAddressingPersonaIdentity(personaDir ...string) string {
	spec := agent.PromptSpec{}
	promptprofile.ApplyPersonaIdentity(&spec, slog.Default(), personaDir...)
	return strings.TrimSpace(spec.Identity)
}
