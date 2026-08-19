package slack

import (
	"strings"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

func slackTaskConversation(job slackJob, botUserID string) *taskdomain.TaskConversation {
	senderName := strings.TrimSpace(job.DisplayName)
	if senderName == "" {
		senderName = strings.TrimSpace(job.Username)
	}
	return runtimecore.BuildTaskConversation(
		slackHistoryScopeKeyForJob(job),
		job.ChatType,
		job.UserID,
		senderName,
		botUserID,
		job.MentionUsers,
	)
}
