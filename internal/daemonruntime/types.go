package daemonruntime

import (
	"context"
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

type SubmitTaskRequest struct {
	Task         string       `json:"task"`
	Model        string       `json:"model,omitempty"`
	LLMProfile   string       `json:"llm_profile,omitempty"`
	Timeout      string       `json:"timeout,omitempty"` // time.ParseDuration; optional
	TopicID      string       `json:"topic_id,omitempty"`
	TopicTitle   string       `json:"topic_title,omitempty"`
	WorkspaceDir string       `json:"workspace_dir,omitempty"`
	Trigger      *TaskTrigger `json:"trigger,omitempty"`
}

type SubmitTaskResponse struct {
	ID      string     `json:"id"`
	Status  TaskStatus `json:"status"`
	TopicID string     `json:"topic_id,omitempty"`
}

type StopTaskRequest struct {
	TaskID  string `json:"task_id,omitempty"`
	TopicID string `json:"topic_id,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type StopTaskResponse struct {
	Status   string `json:"status"`
	Found    bool   `json:"found"`
	TaskID   string `json:"task_id,omitempty"`
	TopicID  string `json:"topic_id,omitempty"`
	Progress string `json:"progress,omitempty"`
	Message  string `json:"message,omitempty"`
}

type ApprovalListFunc func(ctx context.Context, req ApprovalListRequest) (ApprovalListResponse, error)
type ApprovalDecisionFunc func(ctx context.Context, req ApprovalDecisionRequest) (ApprovalDecisionResponse, error)

type ApprovalListRequest struct {
	Status string
	Limit  int
}

type ApprovalInfo struct {
	ApprovalRequestID     string     `json:"approval_request_id"`
	TaskID                string     `json:"task_id,omitempty"`
	RunID                 string     `json:"run_id,omitempty"`
	Status                string     `json:"status"`
	ToolName              string     `json:"tool_name,omitempty"`
	ActionSummaryRedacted string     `json:"action_summary_redacted,omitempty"`
	Reasons               []string   `json:"reasons,omitempty"`
	Runtime               string     `json:"runtime,omitempty"`
	Target                string     `json:"target,omitempty"`
	TopicID               string     `json:"topic_id,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	ExpiresAt             time.Time  `json:"expires_at"`
	PendingAt             *time.Time `json:"pending_at,omitempty"`
}

type ApprovalListResponse struct {
	Items []ApprovalInfo `json:"items"`
	Limit int            `json:"limit,omitempty"`
}

type ApprovalDecisionRequest struct {
	ApprovalRequestID string `json:"approval_request_id,omitempty"`
	Actor             string `json:"actor,omitempty"`
	Note              string `json:"note,omitempty"`
}

type ApprovalDecisionResponse struct {
	ApprovalRequestID string `json:"approval_request_id"`
	TaskID            string `json:"task_id,omitempty"`
	Status            string `json:"status"`
	Resumed           bool   `json:"resumed"`
	Error             string `json:"error,omitempty"`
}

type TaskInfo struct {
	ID                string     `json:"id"`
	Status            TaskStatus `json:"status"`
	Task              string     `json:"task"`
	Model             string     `json:"model"`
	LLMProfile        string     `json:"llm_profile,omitempty"`
	Timeout           string     `json:"timeout"`
	CreatedAt         time.Time  `json:"created_at"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	PendingAt         *time.Time `json:"pending_at,omitempty"`
	ResumedAt         *time.Time `json:"resumed_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	ApprovalRequestID string     `json:"approval_request_id,omitempty"`
	Error             string     `json:"error,omitempty"`
	Result            any        `json:"result,omitempty"`
	TopicID           string     `json:"topic_id,omitempty"`
}

type TaskListResponse struct {
	Items      []TaskInfo `json:"items"`
	Limit      int        `json:"limit,omitempty"`
	NextCursor string     `json:"next_cursor,omitempty"`
	HasNext    bool       `json:"has_next,omitempty"`
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

const (
	ConsoleDefaultTopicID      = "default"
	ConsoleDefaultTopicTitle   = "Default"
	ConsoleAwarenessTopicID    = "_awareness"
	ConsoleAwarenessTopicTitle = "Awareness"
)

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
