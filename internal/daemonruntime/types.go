package daemonruntime

import (
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
	Task       string       `json:"task"`
	Model      string       `json:"model,omitempty"`
	Timeout    string       `json:"timeout,omitempty"` // time.ParseDuration; optional
	TopicID    string       `json:"topic_id,omitempty"`
	TopicTitle string       `json:"topic_title,omitempty"`
	Trigger    *TaskTrigger `json:"trigger,omitempty"`
}

type SubmitTaskResponse struct {
	ID      string     `json:"id"`
	Status  TaskStatus `json:"status"`
	TopicID string     `json:"topic_id,omitempty"`
}

type TaskInfo struct {
	ID                string     `json:"id"`
	Status            TaskStatus `json:"status"`
	Task              string     `json:"task"`
	Model             string     `json:"model"`
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

type TaskTrigger struct {
	Source string `json:"source,omitempty"`
	Event  string `json:"event,omitempty"`
	Ref    string `json:"ref,omitempty"`
}

type TopicInfo struct {
	ID        string     `json:"id"`
	Title     string     `json:"title,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

const (
	ConsoleDefaultTopicID      = "default"
	ConsoleDefaultTopicTitle   = "Default"
	ConsoleHeartbeatTopicTitle = "Heartbeat"
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
