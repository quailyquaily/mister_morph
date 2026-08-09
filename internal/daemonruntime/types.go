package daemonruntime

import (
	"context"
	"time"

	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

type TaskStatus = taskdomain.TaskStatus

const (
	TaskQueued   = taskdomain.TaskQueued
	TaskRunning  = taskdomain.TaskRunning
	TaskPending  = taskdomain.TaskPending
	TaskDone     = taskdomain.TaskDone
	TaskFailed   = taskdomain.TaskFailed
	TaskCanceled = taskdomain.TaskCanceled
)

type SubmitTaskRequest struct {
	Task           string          `json:"task"`
	Model          string          `json:"model,omitempty"`
	LLMProfile     string          `json:"llm_profile,omitempty"`
	Timeout        string          `json:"timeout,omitempty"` // time.ParseDuration; optional
	TopicID        string          `json:"topic_id,omitempty"`
	TopicTitle     string          `json:"topic_title,omitempty"`
	WorkspaceDir   string          `json:"workspace_dir,omitempty"`
	FileReferences []FileReference `json:"file_references,omitempty"`
	Trigger        *TaskTrigger    `json:"trigger,omitempty"`
}

type FileReference = taskdomain.FileReference

type SubmitTaskResponse struct {
	ID                string     `json:"id"`
	Status            TaskStatus `json:"status"`
	TopicID           string     `json:"topic_id,omitempty"`
	SteerTargetTaskID string     `json:"steer_target_task_id,omitempty"`
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

type TaskInfo = taskdomain.TaskInfo

type TaskTrigger = taskdomain.TaskTrigger

type TopicInfo = taskdomain.TopicInfo

const (
	ConsoleDefaultTopicID      = "default"
	ConsoleDefaultTopicTitle   = "Default"
	ConsoleAwarenessTopicID    = "_awareness"
	ConsoleAwarenessTopicTitle = "Awareness"
)
