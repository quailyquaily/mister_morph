package lark

import (
	"strings"
	"time"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/memory"
)

func larkMemorySubjectID(job larkJob) string {
	chatID := strings.TrimSpace(job.ChatID)
	if chatID == "" {
		return ""
	}
	return memory.SanitizeSubjectID("lark--" + chatID)
}

func larkMemorySessionID(job larkJob) string {
	return "lark:" + strings.TrimSpace(job.ChatID)
}

func larkMemoryRequestContext(chatType string) memory.RequestContext {
	return runtimecore.RequestContextFromChatType(chatType, "private", "p2p")
}

func larkMemoryParticipants(job larkJob) []memory.MemoryParticipant {
	return runtimecore.BuildParticipants("lark", job.FromUserID, job.MentionUsers)
}

func buildLarkMemoryHistory(history []chathistory.ChatHistoryItem, job larkJob, output string, sentAt time.Time) []chathistory.ChatHistoryItem {
	inbound := newLarkInboundHistoryItem(job)
	var outbound *chathistory.ChatHistoryItem
	if strings.TrimSpace(output) != "" {
		item := newLarkOutboundAgentHistoryItem(job, output, sentAt)
		outbound = &item
	}
	return runtimecore.BuildHistory(history, inbound, outbound, 0)
}

func larkMemorySessionContext(job larkJob) memory.SessionContext {
	ctx := memory.SessionContext{
		ConversationID:   strings.TrimSpace(job.ChatID),
		ConversationType: strings.ToLower(strings.TrimSpace(job.ChatType)),
		CounterpartyID:   strings.TrimSpace(job.FromUserID),
		CounterpartyName: strings.TrimSpace(job.DisplayName),
	}
	ctx.CounterpartyLabel = larkMemoryCounterpartyLabel(job)
	return ctx
}

func larkMemoryCounterpartyLabel(job larkJob) string {
	return runtimecore.CounterpartyLabel(job.FromUserID, job.DisplayName, "", "lark_user:")
}
