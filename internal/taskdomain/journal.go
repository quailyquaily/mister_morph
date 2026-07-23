package taskdomain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

const (
	JournalDomain = "task"

	JournalTypeTaskUpsert = "task_upsert"
	JournalTypeTaskUpdate = "task_update"

	JournalTypeTopicUpsert       = "topic_upsert"
	JournalTypeTopicTitleUpdated = "topic_title_updated"
	JournalTypeTopicDeleted      = "topic_deleted"
)

type JournalPayload struct {
	At      time.Time    `json:"at"`
	Target  string       `json:"target,omitempty"`
	Trigger *TaskTrigger `json:"trigger,omitempty"`
	Task    *TaskInfo    `json:"task,omitempty"`
	Topic   *TopicInfo   `json:"topic,omitempty"`
}

func NewJournal(dir string, rotateMaxBytes int64) (*domainjournal.Journal, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("journal dir is required")
	}
	return domainjournal.New(domainjournal.JournalOptions{
		Dir:            dir,
		RotateMaxBytes: rotateMaxBytes,
		SyncEachWrite:  true,
	})
}

func AppendJournalEvent(journal *domainjournal.Journal, target string, eventType string, now time.Time, trigger TaskTrigger, task *TaskInfo, topic *TopicInfo) (domainjournal.Cursor, error) {
	if journal == nil {
		return domainjournal.Cursor{}, nil
	}
	target = strings.TrimSpace(target)
	payload := JournalPayload{At: now.UTC(), Target: target}
	if HasTaskTrigger(trigger) {
		trigger = NormalizeTaskTrigger(trigger)
		payload.Trigger = &trigger
	}
	if task != nil {
		copy := normalizeJournalTask(*task, now)
		payload.Task = &copy
	}
	if topic != nil {
		copy := normalizeJournalTopic(*topic)
		payload.Topic = &copy
	}
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return domainjournal.Cursor{}, fmt.Errorf("encode task journal payload: %w", err)
	}
	trace := domainjournal.Trace{
		TraceID: strings.TrimSpace(trigger.TraceID),
		Runtime: target,
		Target:  target,
	}
	if trace.TraceID == "" && payload.Task != nil {
		trace.TraceID = payload.Task.ID
	}
	if payload.Task != nil {
		trace.TaskID = payload.Task.ID
		trace.TopicID = payload.Task.TopicID
	}
	if trace.TopicID == "" && payload.Topic != nil {
		trace.TopicID = payload.Topic.ID
	}
	return journal.Append(domainjournal.Event{
		ID:            "evt_" + uuid.NewString(),
		Time:          now.UTC().Format(time.RFC3339Nano),
		Domain:        JournalDomain,
		Type:          strings.TrimSpace(eventType),
		SchemaVersion: 1,
		Trace:         trace,
		Payload:       payloadRaw,
	})
}

func normalizeJournalTask(task TaskInfo, now time.Time) TaskInfo {
	task.ID = strings.TrimSpace(task.ID)
	task.Task = strings.TrimSpace(task.Task)
	task.Model = strings.TrimSpace(task.Model)
	task.LLMProfile = strings.TrimSpace(task.LLMProfile)
	task.Timeout = strings.TrimSpace(task.Timeout)
	task.Error = strings.TrimSpace(task.Error)
	task.TopicID = strings.TrimSpace(task.TopicID)
	task.SteerTargetTaskID = strings.TrimSpace(task.SteerTargetTaskID)
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now.UTC()
	} else {
		task.CreatedAt = task.CreatedAt.UTC()
	}
	for _, value := range []**time.Time{
		&task.StartedAt,
		&task.PendingAt,
		&task.ResumedAt,
		&task.FinishedAt,
	} {
		if *value != nil {
			normalized := (**value).UTC()
			*value = &normalized
		}
	}
	return task
}

func normalizeJournalTopic(topic TopicInfo) TopicInfo {
	topic.ID = strings.TrimSpace(topic.ID)
	topic.Title = strings.TrimSpace(topic.Title)
	if !topic.CreatedAt.IsZero() {
		topic.CreatedAt = topic.CreatedAt.UTC()
	}
	if !topic.UpdatedAt.IsZero() {
		topic.UpdatedAt = topic.UpdatedAt.UTC()
	}
	for _, value := range []**time.Time{&topic.LLMTitleGeneratedAt, &topic.DeletedAt} {
		if *value != nil {
			normalized := (**value).UTC()
			*value = &normalized
		}
	}
	return topic
}

func DecodeJournalPayload(raw json.RawMessage) (JournalPayload, error) {
	var payload JournalPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return JournalPayload{}, fmt.Errorf("decode task journal payload: %w", err)
	}
	return payload, nil
}
