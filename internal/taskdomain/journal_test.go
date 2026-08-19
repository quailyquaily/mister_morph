package taskdomain

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

func TestJournalCodecRoundTrip(t *testing.T) {
	dir := t.TempDir()
	journal, err := NewJournal(dir, 1024*1024)
	if err != nil {
		t.Fatalf("NewJournal() error = %v", err)
	}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	trigger := TaskTrigger{Source: "integration", Event: "run_task", Ref: "task_1", TraceID: "trace_1"}
	task := TaskInfo{
		ID:        " task_1 ",
		Status:    TaskDone,
		Task:      " ping ",
		Model:     " gpt-test ",
		CreatedAt: now,
		TopicID:   " topic_1 ",
		Conversation: &TaskConversation{
			ConversationID:   " tg:42 ",
			ConversationType: " SUPERGROUP ",
			Participants: []TaskParticipant{
				{ID: " @alice ", Nickname: " Alice "},
				{ID: "", Nickname: "drop"},
				{ID: "@alice", Nickname: "duplicate"},
				{ID: " @bob "},
			},
		},
	}
	topic := TopicInfo{ID: "topic_1", Title: "Topic", CreatedAt: now, UpdatedAt: now}
	if _, err := AppendJournalEvent(journal, "integration", JournalTypeTaskUpsert, now, trigger, &task, &topic); err != nil {
		t.Fatalf("AppendJournalEvent() error = %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var records []domainjournal.Record
	if err := domainjournal.ReplayDir(dir, func(record domainjournal.Record) error {
		records = append(records, record)
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record.Event.Domain != JournalDomain || record.Event.Type != JournalTypeTaskUpsert {
		t.Fatalf("event domain/type = %q/%q", record.Event.Domain, record.Event.Type)
	}
	if record.Event.Trace.TraceID != "trace_1" || record.Event.Trace.TaskID != "task_1" || record.Event.Trace.TopicID != "topic_1" {
		t.Fatalf("trace = %+v", record.Event.Trace)
	}
	payload, err := DecodeJournalPayload(record.Event.Payload)
	if err != nil {
		t.Fatalf("DecodeJournalPayload() error = %v", err)
	}
	if payload.Target != "integration" || payload.Task == nil || payload.Task.ID != "task_1" || payload.Topic == nil || payload.Topic.ID != "topic_1" {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Task.Task != "ping" || payload.Task.Model != "gpt-test" || payload.Task.TopicID != "topic_1" {
		t.Fatalf("normalized task = %+v", payload.Task)
	}
	conversation := payload.Task.Conversation
	if conversation == nil {
		t.Fatal("normalized conversation is nil")
	}
	if conversation.ConversationID != "tg:42" || conversation.ConversationType != "supergroup" {
		t.Fatalf("normalized conversation = %+v", conversation)
	}
	wantParticipants := []TaskParticipant{
		{ID: "@alice", Nickname: "Alice"},
		{ID: "@bob"},
	}
	if len(conversation.Participants) != len(wantParticipants) {
		t.Fatalf("participants = %+v, want %+v", conversation.Participants, wantParticipants)
	}
	for i, want := range wantParticipants {
		if got := conversation.Participants[i]; got != want {
			t.Fatalf("participants[%d] = %+v, want %+v", i, got, want)
		}
	}
	if payload.Trigger == nil || *payload.Trigger != trigger {
		t.Fatalf("payload trigger = %+v, want %+v", payload.Trigger, trigger)
	}
}
