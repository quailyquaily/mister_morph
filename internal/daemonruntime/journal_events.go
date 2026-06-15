package daemonruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

const (
	taskJournalDomain = "task"

	taskJournalTypeTaskUpsert = "task_upsert"
	taskJournalTypeTaskUpdate = "task_update"

	taskJournalTypeTopicUpsert       = "topic_upsert"
	taskJournalTypeTopicTitleUpdated = "topic_title_updated"
	taskJournalTypeTopicDeleted      = "topic_deleted"
)

type taskJournalPayload struct {
	At      time.Time    `json:"at"`
	Target  string       `json:"target,omitempty"`
	Trigger *TaskTrigger `json:"trigger,omitempty"`
	Task    *TaskInfo    `json:"task,omitempty"`
	Topic   *TopicInfo   `json:"topic,omitempty"`
}

func newTaskDomainJournal(dir string, rotateMaxBytes int64) (*domainjournal.Journal, error) {
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

func appendTaskDomainEvent(journal *domainjournal.Journal, target string, typ string, now time.Time, trigger TaskTrigger, task *TaskInfo, topic *TopicInfo) (domainjournal.Cursor, error) {
	if journal == nil {
		return domainjournal.Cursor{}, nil
	}
	target = strings.TrimSpace(target)
	payload := taskJournalPayload{
		At:     now.UTC(),
		Target: target,
	}
	if hasTaskTrigger(trigger) {
		trigger = normalizeTaskTrigger(trigger)
		payload.Trigger = &trigger
	}
	if task != nil {
		cp := *task
		payload.Task = &cp
	}
	if topic != nil {
		cp := normalizeTopicInfo(*topic)
		payload.Topic = &cp
	}
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return domainjournal.Cursor{}, fmt.Errorf("encode task journal payload: %w", err)
	}
	trace := domainjournal.Trace{
		TraceID: traceIDFromTaskEvent(trigger, task),
		Runtime: target,
		Target:  target,
	}
	if task != nil {
		trace.TaskID = strings.TrimSpace(task.ID)
		trace.TopicID = strings.TrimSpace(task.TopicID)
	}
	if trace.TopicID == "" && topic != nil {
		trace.TopicID = strings.TrimSpace(topic.ID)
	}
	return journal.Append(domainjournal.Event{
		ID:            "evt_" + uuid.NewString(),
		Time:          now.UTC().Format(time.RFC3339Nano),
		Domain:        taskJournalDomain,
		Type:          typ,
		SchemaVersion: 1,
		Trace:         trace,
		Payload:       payloadRaw,
	})
}

func traceIDFromTaskEvent(trigger TaskTrigger, task *TaskInfo) string {
	if traceID := strings.TrimSpace(trigger.TraceID); traceID != "" {
		return traceID
	}
	if task != nil {
		return strings.TrimSpace(task.ID)
	}
	return ""
}
