package taskdomain

import (
	"context"
	"errors"
	"strings"
	"time"
)

type TaskStatus string

const (
	TaskQueued   TaskStatus = "queued"
	TaskRunning  TaskStatus = "running"
	TaskPending  TaskStatus = "pending"
	TaskDone     TaskStatus = "done"
	TaskFailed   TaskStatus = "failed"
	TaskCanceled TaskStatus = "canceled"
)

type FileReference struct {
	DirName string `json:"dir_name"`
	Path    string `json:"path"`
}

type TaskConversation struct {
	ConversationID   string            `json:"conversation_id,omitempty"`
	ConversationType string            `json:"conversation_type,omitempty"`
	Participants     []TaskParticipant `json:"participants,omitempty"`
}

type TaskParticipant struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname,omitempty"`
}

func EndedByCancellation(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "context deadline exceeded") || strings.Contains(message, "context canceled")
}

type TaskInfo struct {
	ID                string            `json:"id"`
	Status            TaskStatus        `json:"status"`
	Task              string            `json:"task"`
	Model             string            `json:"model"`
	LLMProfile        string            `json:"llm_profile,omitempty"`
	Timeout           string            `json:"timeout"`
	CreatedAt         time.Time         `json:"created_at"`
	StartedAt         *time.Time        `json:"started_at,omitempty"`
	PendingAt         *time.Time        `json:"pending_at,omitempty"`
	ResumedAt         *time.Time        `json:"resumed_at,omitempty"`
	FinishedAt        *time.Time        `json:"finished_at,omitempty"`
	ApprovalRequestID string            `json:"approval_request_id,omitempty"`
	Error             string            `json:"error,omitempty"`
	Result            any               `json:"result,omitempty"`
	TopicID           string            `json:"topic_id,omitempty"`
	SteerTargetTaskID string            `json:"steer_target_task_id,omitempty"`
	FileReferences    []FileReference   `json:"file_references,omitempty"`
	Conversation      *TaskConversation `json:"conversation,omitempty"`
}

type TaskTrigger struct {
	Source  string `json:"source,omitempty"`
	Event   string `json:"event,omitempty"`
	Ref     string `json:"ref,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

type TopicInfo struct {
	ID                  string     `json:"id"`
	Title               string     `json:"title,omitempty"`
	LLMTitleGeneratedAt *time.Time `json:"llm_title_generated_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DeletedAt           *time.Time `json:"deleted_at,omitempty"`
}

func ParseTaskStatus(raw string) (TaskStatus, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return "", true
	case string(TaskQueued):
		return TaskQueued, true
	case string(TaskRunning):
		return TaskRunning, true
	case string(TaskPending):
		return TaskPending, true
	case string(TaskDone):
		return TaskDone, true
	case string(TaskFailed):
		return TaskFailed, true
	case string(TaskCanceled):
		return TaskCanceled, true
	default:
		return "", false
	}
}

func NormalizeTaskTrigger(trigger TaskTrigger) TaskTrigger {
	return TaskTrigger{
		Source:  strings.TrimSpace(trigger.Source),
		Event:   strings.TrimSpace(trigger.Event),
		Ref:     strings.TrimSpace(trigger.Ref),
		TraceID: strings.TrimSpace(trigger.TraceID),
	}
}

func HasTaskTrigger(trigger TaskTrigger) bool {
	return strings.TrimSpace(trigger.Source) != "" ||
		strings.TrimSpace(trigger.Event) != "" ||
		strings.TrimSpace(trigger.Ref) != "" ||
		strings.TrimSpace(trigger.TraceID) != ""
}

func NormalizeTaskConversation(conversation *TaskConversation) *TaskConversation {
	if conversation == nil {
		return nil
	}
	normalized := &TaskConversation{
		ConversationID:   strings.TrimSpace(conversation.ConversationID),
		ConversationType: strings.ToLower(strings.TrimSpace(conversation.ConversationType)),
	}
	seen := make(map[string]bool, len(conversation.Participants))
	for _, participant := range conversation.Participants {
		participant.ID = strings.TrimSpace(participant.ID)
		participant.Nickname = strings.TrimSpace(participant.Nickname)
		if participant.ID == "" || seen[participant.ID] {
			continue
		}
		seen[participant.ID] = true
		normalized.Participants = append(normalized.Participants, participant)
	}
	if normalized.ConversationID == "" && normalized.ConversationType == "" && len(normalized.Participants) == 0 {
		return nil
	}
	return normalized
}
