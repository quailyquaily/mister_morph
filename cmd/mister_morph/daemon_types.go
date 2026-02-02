package main

import "time"

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
	Task    string `json:"task"`
	Model   string `json:"model,omitempty"`
	Timeout string `json:"timeout,omitempty"` // time.ParseDuration; optional
}

type SubmitTaskResponse struct {
	ID     string     `json:"id"`
	Status TaskStatus `json:"status"`
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
}
