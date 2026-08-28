package mixin

import (
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

func mixinTaskConversation(job mixinJob) *taskdomain.TaskConversation {
	return runtimecore.BuildTaskConversation(job.ConversationKey, job.ChatType, job.FromUserID, job.DisplayName, job.IdentityNumber, job.MentionUsers)
}
