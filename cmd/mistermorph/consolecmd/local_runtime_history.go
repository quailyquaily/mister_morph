package consolecmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/llm"
)

const (
	consoleHistoryRestoreTaskLimit = 6
	consoleHistoryRestoreScanLimit = 200
	consoleHistoryChannel          = "console"
	consoleAgentUserID             = "0"
	consoleAgentUsername           = "agent"
	consoleAgentNickname           = "MisterMorph"
)

func (r *consoleLocalRuntime) buildConsolePromptMessages(job consoleLocalTaskJob) ([]llm.Message, *llm.Message, error) {
	history := r.loadConsoleTopicHistory(job)
	return renderConsolePromptMessages(history, job)
}

func (r *consoleLocalRuntime) loadConsoleTopicHistory(job consoleLocalTaskJob) []chathistory.ChatHistoryItem {
	if r == nil || r.store == nil {
		return nil
	}
	topicID := strings.TrimSpace(job.TopicID)
	if topicID == "" {
		topicID = daemonruntime.ConsoleDefaultTopicID
	}
	tasks := r.store.List(daemonruntime.TaskListOptions{
		TopicID: topicID,
		Limit:   consoleHistoryRestoreScanLimit,
	})
	return buildConsoleTopicHistory(tasks, job, consoleHistoryRestoreTaskLimit)
}

func renderConsolePromptMessages(history []chathistory.ChatHistoryItem, job consoleLocalTaskJob) ([]llm.Message, *llm.Message, error) {
	historyRaw, err := chathistory.RenderHistoryContext(consoleHistoryChannel, history)
	if err != nil {
		return nil, nil, fmt.Errorf("render console history context: %w", err)
	}
	historyMsgs := make([]llm.Message, 0, 1)
	if strings.TrimSpace(historyRaw) != "" {
		historyMsgs = append(historyMsgs, llm.Message{
			Role:    "user",
			Content: historyRaw,
		})
	}
	currentRaw, err := chathistory.RenderCurrentMessage(newConsoleInboundHistoryItem(job))
	if err != nil {
		return nil, nil, fmt.Errorf("render console current message: %w", err)
	}
	currentMsg := &llm.Message{
		Role:    "user",
		Content: currentRaw,
	}
	return historyMsgs, currentMsg, nil
}

func buildConsoleTopicHistory(tasks []daemonruntime.TaskInfo, job consoleLocalTaskJob, limit int) []chathistory.ChatHistoryItem {
	if limit <= 0 || len(tasks) == 0 {
		return nil
	}
	prior := make([]daemonruntime.TaskInfo, 0, limit)
	for _, task := range tasks {
		if !consoleTaskPrecedesJob(task, job) {
			continue
		}
		userText := strings.TrimSpace(task.Task)
		replyText := consoleTaskReplyText(task)
		if userText == "" && replyText == "" {
			continue
		}
		prior = append(prior, task)
		if len(prior) == limit {
			break
		}
	}
	for left, right := 0, len(prior)-1; left < right; left, right = left+1, right-1 {
		prior[left], prior[right] = prior[right], prior[left]
	}
	history := make([]chathistory.ChatHistoryItem, 0, len(prior)*2)
	for _, task := range prior {
		if inbound := newConsoleInboundHistoryItemFromTask(task); strings.TrimSpace(inbound.Text) != "" {
			history = append(history, inbound)
		}
		if outbound, ok := newConsoleOutboundHistoryItemFromTask(task); ok {
			history = append(history, outbound)
		}
	}
	return history
}

func consoleTaskPrecedesJob(task daemonruntime.TaskInfo, job consoleLocalTaskJob) bool {
	taskID := strings.TrimSpace(task.ID)
	if taskID == "" || taskID == strings.TrimSpace(job.TaskID) {
		return false
	}
	if job.CreatedAt.IsZero() {
		return true
	}
	if task.CreatedAt.IsZero() {
		return false
	}
	return task.CreatedAt.UTC().Before(job.CreatedAt.UTC())
}

func newConsoleInboundHistoryItem(job consoleLocalTaskJob) chathistory.ChatHistoryItem {
	sentAt := job.CreatedAt.UTC()
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}
	return chathistory.ChatHistoryItem{
		Channel:   consoleHistoryChannel,
		Kind:      chathistory.KindInboundUser,
		ChatID:    buildConsoleHistoryChatID(job.TopicID),
		ChatType:  "private",
		MessageID: strings.TrimSpace(job.TaskID),
		SentAt:    sentAt,
		Sender: chathistory.ChatHistorySender{
			UserID:     consoleParticipantKey,
			Username:   consoleUsername,
			Nickname:   consoleDisplayName,
			DisplayRef: consoleParticipantKey,
		},
		Text: strings.TrimSpace(job.Task),
	}
}

func newConsoleInboundHistoryItemFromTask(task daemonruntime.TaskInfo) chathistory.ChatHistoryItem {
	return chathistory.ChatHistoryItem{
		Channel:   consoleHistoryChannel,
		Kind:      chathistory.KindInboundUser,
		ChatID:    buildConsoleHistoryChatID(task.TopicID),
		ChatType:  "private",
		MessageID: strings.TrimSpace(task.ID),
		SentAt:    consoleTaskInboundSentAt(task),
		Sender: chathistory.ChatHistorySender{
			UserID:     consoleParticipantKey,
			Username:   consoleUsername,
			Nickname:   consoleDisplayName,
			DisplayRef: consoleParticipantKey,
		},
		Text: strings.TrimSpace(task.Task),
	}
}

func newConsoleOutboundHistoryItemFromTask(task daemonruntime.TaskInfo) (chathistory.ChatHistoryItem, bool) {
	text := consoleTaskReplyText(task)
	if text == "" {
		return chathistory.ChatHistoryItem{}, false
	}
	return chathistory.ChatHistoryItem{
		Channel:          consoleHistoryChannel,
		Kind:             chathistory.KindOutboundAgent,
		ChatID:           buildConsoleHistoryChatID(task.TopicID),
		ChatType:         "private",
		ReplyToMessageID: strings.TrimSpace(task.ID),
		SentAt:           consoleTaskOutboundSentAt(task),
		Sender: chathistory.ChatHistorySender{
			UserID:     consoleAgentUserID,
			Username:   consoleAgentUsername,
			Nickname:   consoleAgentNickname,
			IsBot:      true,
			DisplayRef: consoleAgentUsername,
		},
		Text: text,
	}, true
}

func buildConsoleHistoryChatID(topicID string) string {
	return buildConsoleConversationKey(strings.TrimSpace(topicID))
}

func consoleTaskInboundSentAt(task daemonruntime.TaskInfo) time.Time {
	sentAt := task.CreatedAt.UTC()
	if sentAt.IsZero() {
		return time.Now().UTC()
	}
	return sentAt
}

func consoleTaskOutboundSentAt(task daemonruntime.TaskInfo) time.Time {
	if task.FinishedAt != nil && !task.FinishedAt.IsZero() {
		return task.FinishedAt.UTC()
	}
	if task.ResumedAt != nil && !task.ResumedAt.IsZero() {
		return task.ResumedAt.UTC()
	}
	if task.PendingAt != nil && !task.PendingAt.IsZero() {
		return task.PendingAt.UTC()
	}
	if task.StartedAt != nil && !task.StartedAt.IsZero() {
		return task.StartedAt.UTC()
	}
	return consoleTaskInboundSentAt(task)
}

func consoleTaskReplyText(task daemonruntime.TaskInfo) string {
	if text := consoleTaskResultOutput(task.Result); text != "" {
		return text
	}
	return strings.TrimSpace(task.Error)
}

func consoleTaskResultOutput(result any) string {
	switch value := result.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case agent.Final:
		return consoleTaskResultOutput(value.Output)
	case *agent.Final:
		if value == nil {
			return ""
		}
		return consoleTaskResultOutput(value.Output)
	case map[string]any:
		if nested, ok := value["final"]; ok {
			if text := consoleTaskResultOutput(nested); text != "" {
				return text
			}
		}
		if output, ok := value["output"]; ok {
			return stringifyConsoleTaskResultValue(output)
		}
	}
	return ""
}

func stringifyConsoleTaskResultValue(value any) string {
	switch raw := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(raw)
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(raw))
		}
		return strings.TrimSpace(string(data))
	}
}
