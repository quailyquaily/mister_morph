package slack

import (
	"fmt"
	"strings"
	"time"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/memory"
)

func slackMemorySubjectID(job slackJob) string {
	teamID := strings.ToLower(strings.TrimSpace(job.TeamID))
	channelID := strings.ToLower(strings.TrimSpace(job.ChannelID))
	if teamID == "" || channelID == "" {
		return ""
	}
	return memory.SanitizeSubjectID("slack--" + teamID + "--" + channelID)
}

func slackMemorySessionID(job slackJob) string {
	teamID := strings.TrimSpace(job.TeamID)
	channelID := strings.TrimSpace(job.ChannelID)
	return fmt.Sprintf("slack:%s:%s", teamID, channelID)
}

func slackMemoryTaskRunID(job slackJob) string {
	return strings.TrimSpace(job.TaskID)
}

func slackMemoryRequestContext(chatType string) memory.RequestContext {
	return runtimecore.RequestContextFromChatType(chatType, "im")
}

func slackMemoryParticipants(job slackJob) []memory.MemoryParticipant {
	return runtimecore.BuildParticipants("slack", job.UserID, job.MentionUsers)
}

func buildSlackMemoryHistory(history []chathistory.ChatHistoryItem, job slackJob, output string, sentAt time.Time, maxItems int) []chathistory.ChatHistoryItem {
	inbound := newSlackInboundHistoryItem(job)
	var outbound *chathistory.ChatHistoryItem
	if strings.TrimSpace(output) != "" {
		item := newSlackOutboundAgentHistoryItem(job, output, sentAt, "")
		outbound = &item
	}
	return runtimecore.BuildHistory(history, inbound, outbound, maxItems)
}

func slackMemorySessionContext(job slackJob) memory.SessionContext {
	ctx := memory.SessionContext{
		ConversationID:   strings.TrimSpace(job.ChannelID),
		ConversationType: strings.ToLower(strings.TrimSpace(job.ChatType)),
		CounterpartyID:   strings.TrimSpace(job.UserID),
		CounterpartyName: strings.TrimSpace(job.DisplayName),
	}
	if username := strings.TrimSpace(job.Username); username != "" {
		ctx.CounterpartyHandle = username
	}
	ctx.CounterpartyLabel = slackMemoryCounterpartyLabel(job)
	return ctx
}

func slackMemoryCounterpartyLabel(job slackJob) string {
	return runtimecore.CounterpartyLabel(job.UserID, job.DisplayName, job.Username, "slack:")
}
