package core

import (
	"strings"

	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/memory"
)

func RequestContextFromChatType(chatType string, privateTypes ...string) memory.RequestContext {
	normalized := strings.ToLower(strings.TrimSpace(chatType))
	for _, raw := range privateTypes {
		if normalized == strings.ToLower(strings.TrimSpace(raw)) {
			return memory.ContextPrivate
		}
	}
	return memory.ContextPublic
}

func BuildParticipants(protocol, primaryID string, mentionIDs []string) []memory.MemoryParticipant {
	seen := map[string]bool{}
	out := make([]memory.MemoryParticipant, 0, 1+len(mentionIDs))
	appendParticipant := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, memory.MemoryParticipant{
			ID:       id,
			Nickname: id,
			Protocol: strings.TrimSpace(protocol),
		})
	}
	appendParticipant(primaryID)
	for _, id := range mentionIDs {
		appendParticipant(id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func BuildHistory(
	history []chathistory.ChatHistoryItem,
	inbound chathistory.ChatHistoryItem,
	outbound *chathistory.ChatHistoryItem,
	maxItems int,
) []chathistory.ChatHistoryItem {
	out := append([]chathistory.ChatHistoryItem{}, history...)
	out = append(out, inbound)
	if outbound != nil {
		out = append(out, *outbound)
	}
	if maxItems > 0 && len(out) > maxItems {
		out = out[len(out)-maxItems:]
	}
	return out
}

func CounterpartyLabel(id, name, fallbackName, refPrefix string) string {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(fallbackName)
	}
	refPrefix = strings.TrimSpace(refPrefix)
	ref := ""
	if id != "" && refPrefix != "" {
		ref = refPrefix + id
	}
	if name != "" && ref != "" {
		return "[" + name + "](" + ref + ")"
	}
	if name != "" {
		return name
	}
	return ref
}
