package line

import (
	"strings"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

func lineTaskConversation(job lineJob, botUserID string) *taskdomain.TaskConversation {
	senderName := strings.TrimSpace(job.DisplayName)
	if senderName == "" {
		senderName = strings.TrimSpace(job.FromUsername)
	}
	return runtimecore.BuildTaskConversation(
		job.ConversationKey,
		job.ChatType,
		job.FromUserID,
		senderName,
		botUserID,
		job.MentionUsers,
	)
}
