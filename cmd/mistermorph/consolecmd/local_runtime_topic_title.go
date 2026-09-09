package consolecmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/textutil"
)

func (r *consoleLocalRuntime) regenerateTopicTitle(ctx context.Context, generation *consoleLocalRuntimeGeneration, topicID string) (daemonruntime.TopicInfo, error) {
	if r == nil || r.store == nil || generation == nil {
		return daemonruntime.TopicInfo{}, fmt.Errorf("console runtime generation is not initialized")
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" || topicID == daemonruntime.ConsoleDefaultTopicID || topicID == daemonruntime.ConsoleAwarenessTopicID {
		return daemonruntime.TopicInfo{}, daemonruntime.BadRequest("topic cannot be renamed")
	}
	if _, busy := r.topicTitleRegenerations.LoadOrStore(topicID, struct{}{}); busy {
		return daemonruntime.TopicInfo{}, daemonruntime.ErrTopicTitleBusy
	}
	defer r.topicTitleRegenerations.Delete(topicID)

	input := consoleTopicTitleInput(r.store.TopicTitleTasks(topicID, 6))
	if input == "" {
		return daemonruntime.TopicInfo{}, daemonruntime.BadRequest("topic has no conversation to name")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if r.consoleExecutionState != nil && r.workersCtx != nil {
		stop := context.AfterFunc(r.workersCtx, cancel)
		defer stop()
	}
	if err := ctx.Err(); err != nil {
		return daemonruntime.TopicInfo{}, err
	}
	started, err := r.store.BeginTopicTitleRegeneration(topicID)
	if err != nil {
		return daemonruntime.TopicInfo{}, err
	}
	generation.acquire()
	defer generation.release()
	title, err := r.generateTopicTitle(ctx, generation, input)
	if err != nil {
		return daemonruntime.TopicInfo{}, err
	}
	if err := ctx.Err(); err != nil {
		return daemonruntime.TopicInfo{}, err
	}
	return r.store.CompleteTopicTitleRegeneration(topicID, started.TitleRevision, title.Title, title.Icon)
}

func consoleTopicTitleInput(tasks []daemonruntime.TaskInfo) string {
	var input strings.Builder
	for _, task := range tasks {
		text := strings.TrimSpace(task.Task)
		if text == "" {
			continue
		}
		input.WriteString("User: ")
		input.WriteString(textutil.TruncateRunes(text, 600))
		input.WriteByte('\n')
		if task.Status != daemonruntime.TaskDone || task.SteerTargetTaskID != "" {
			continue
		}
		if reply := consoleTaskResultOutput(task.Result); reply != "" {
			input.WriteString("Assistant: ")
			input.WriteString(textutil.TruncateRunes(reply, 400))
			input.WriteByte('\n')
		}
	}
	return strings.TrimSpace(input.String())
}
