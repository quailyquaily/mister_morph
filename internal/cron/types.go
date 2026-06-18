package cron

import (
	"strings"
	"time"
)

const (
	DefaultFilename  = "cron.yaml"
	DefaultTaskTitle = "Unititled TODO"
	Version          = 1

	TimestampLayout = "2006-01-02 15:04"
)

type File struct {
	Version int    `yaml:"version"`
	Tasks   []Task `yaml:"tasks"`
}

type Task struct {
	ID      string `yaml:"id" json:"id"`
	Title   string `yaml:"title,omitempty" json:"title,omitempty"`
	Enabled *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	At      string `yaml:"at,omitempty" json:"at,omitempty"`
	Cron    string `yaml:"cron,omitempty" json:"cron,omitempty"`
	TZ      string `yaml:"tz,omitempty" json:"tz,omitempty"`
	Content string `yaml:"content" json:"content"`
	ChatID  string `yaml:"chat_id,omitempty" json:"chat_id,omitempty"`
	Mention string `yaml:"mention,omitempty" json:"mention,omitempty"`
}

type DueTask struct {
	Task           Task
	ScheduledAtUTC time.Time
	Manual         bool
}

type AddResult struct {
	OK        bool     `json:"ok"`
	Action    string   `json:"action"`
	TaskCount int      `json:"task_count"`
	Task      *Task    `json:"task,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

type DeleteResult struct {
	OK        bool   `json:"ok"`
	Action    string `json:"action"`
	TaskCount int    `json:"task_count"`
	Deleted   *Task  `json:"deleted,omitempty"`
}

func ScheduleForTask(task Task) string {
	if strings.TrimSpace(task.At) != "" {
		return strings.TrimSpace(task.At)
	}
	return strings.TrimSpace(task.Cron)
}

func TaskEnabled(task Task) bool {
	return task.Enabled == nil || *task.Enabled
}

func normalizeTaskTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return DefaultTaskTitle
	}
	return title
}
