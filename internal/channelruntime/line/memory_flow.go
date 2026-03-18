package line

import (
	"strings"
	"time"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/memory"
)

func lineMemorySubjectID(job lineJob) string {
	chatID := strings.TrimSpace(job.ChatID)
	if chatID == "" {
		return ""
	}
	return memory.SanitizeSubjectID("line--" + chatID)
}

func lineMemorySessionID(job lineJob) string {
	return "line:" + strings.TrimSpace(job.ChatID)
}

func lineMemoryRequestContext(chatType string) memory.RequestContext {
	return runtimecore.RequestContextFromChatType(chatType, "private", "user")
}

func lineMemoryParticipants(job lineJob) []memory.MemoryParticipant {
	return runtimecore.BuildParticipants("line", job.FromUserID, job.MentionUsers)
}

func buildLineMemoryHistory(history []chathistory.ChatHistoryItem, job lineJob, output string, sentAt time.Time) []chathistory.ChatHistoryItem {
	inbound := newLineInboundHistoryItem(job)
	var outbound *chathistory.ChatHistoryItem
	if strings.TrimSpace(output) != "" {
		item := newLineOutboundAgentHistoryItem(job, output, sentAt)
		outbound = &item
	}
	return runtimecore.BuildHistory(history, inbound, outbound, 0)
}

func lineMemorySessionContext(job lineJob) memory.SessionContext {
	ctx := memory.SessionContext{
		ConversationID:   strings.TrimSpace(job.ChatID),
		ConversationType: strings.ToLower(strings.TrimSpace(job.ChatType)),
		CounterpartyID:   strings.TrimSpace(job.FromUserID),
		CounterpartyName: strings.TrimSpace(job.DisplayName),
	}
	if username := strings.TrimSpace(job.FromUsername); username != "" {
		ctx.CounterpartyHandle = username
	}
	ctx.CounterpartyLabel = lineMemoryCounterpartyLabel(job)
	return ctx
}

func lineMemoryCounterpartyLabel(job lineJob) string {
	return runtimecore.CounterpartyLabel(job.FromUserID, job.DisplayName, job.FromUsername, "line_user:")
}
