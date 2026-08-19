package lark

import (
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

func larkTaskConversation(job larkJob) *taskdomain.TaskConversation {
	return runtimecore.BuildTaskConversation(
		job.ConversationKey,
		job.ChatType,
		job.FromUserID,
		job.DisplayName,
		"",
		job.MentionUsers,
	)
}
