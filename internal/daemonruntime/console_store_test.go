package daemonruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/pagination"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

func TestConsoleFileStoreReplayAndAwarenessFiltering(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")

	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	pendingAt := mustParseTime(t, "2026-03-15T10:00:30Z")
	store.UpsertWithTrigger(TaskInfo{
		ID:                "task_default",
		Status:            TaskPending,
		Task:              "hello",
		Model:             "gpt-5.2",
		Timeout:           "10m0s",
		CreatedAt:         mustParseTime(t, "2026-03-15T10:00:00Z"),
		PendingAt:         &pendingAt,
		ApprovalRequestID: "apr_restart",
		Result:            map[string]any{"pending": true},
		TopicID:           ConsoleDefaultTopicID,
	}, TaskTrigger{Source: "ui", Event: "chat_submit"}, "")
	store.UpsertWithTrigger(TaskInfo{
		ID:        "task_awareness",
		Status:    TaskQueued,
		Task:      "awareness",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: mustParseTime(t, "2026-03-15T10:01:00Z"),
		TopicID:   ConsoleAwarenessTopicID,
	}, TaskTrigger{Source: "heartbeat", Event: "heartbeat_tick"}, ConsoleAwarenessTopicTitle)

	visible := store.List(TaskListOptions{Limit: 20})
	if len(visible) != 1 {
		t.Fatalf("len(visible) = %d, want 1", len(visible))
	}
	if visible[0].ID != "task_default" {
		t.Fatalf("visible[0].ID = %q, want task_default", visible[0].ID)
	}

	awarenessItems := store.List(TaskListOptions{Limit: 20, TopicID: ConsoleAwarenessTopicID})
	if len(awarenessItems) != 1 {
		t.Fatalf("len(awarenessItems) = %d, want 1", len(awarenessItems))
	}
	if awarenessItems[0].ID != "task_awareness" {
		t.Fatalf("awarenessItems[0].ID = %q, want task_awareness", awarenessItems[0].ID)
	}

	reloaded, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}

	reloadedDefault, ok := reloaded.Get("task_default")
	if !ok || reloadedDefault == nil {
		t.Fatalf("reloaded default task missing")
	}
	if reloadedDefault.Status != TaskCanceled {
		t.Fatalf("reloaded default status = %q, want %q", reloadedDefault.Status, TaskCanceled)
	}
	if reloadedDefault.Error != "runtime restarted" {
		t.Fatalf("reloaded default error = %q, want runtime restarted", reloadedDefault.Error)
	}
	if reloadedDefault.PendingAt != nil || reloadedDefault.ApprovalRequestID != "" || reloadedDefault.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared after restart", reloadedDefault.PendingAt, reloadedDefault.ApprovalRequestID, reloadedDefault.Result)
	}

	topics := reloaded.ListTopicsPage(TopicListOptions{Limit: 10})
	if len(topics) != 2 {
		t.Fatalf("len(topics) = %d, want 2", len(topics))
	}
	if topics[0].ID != ConsoleAwarenessTopicID {
		t.Fatalf("topics[0].ID = %q, want %q", topics[0].ID, ConsoleAwarenessTopicID)
	}
	if topics[0].Title != ConsoleAwarenessTopicTitle {
		t.Fatalf("topics[0].Title = %q, want %q", topics[0].Title, ConsoleAwarenessTopicTitle)
	}
}

func TestConsoleFileStoreSnapshotAdvancesPastForeignDomainEvents(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	raw, err := domainjournal.New(domainjournal.JournalOptions{Dir: journalDir})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	want, err := raw.Append(domainjournal.Event{
		ID:            "evt_conversation",
		Time:          "2026-08-07T03:04:05Z",
		Domain:        "conversation",
		Type:          "untriggered_inbound",
		SchemaVersion: 1,
		Payload:       []byte(`{"message_id":"42"}`),
	})
	if err != nil {
		t.Fatalf("Append(conversation) error = %v", err)
	}
	want.Line = 1
	if err := raw.Close(); err != nil {
		t.Fatalf("raw.Close() error = %v", err)
	}

	if _, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	}); err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	snap, ok, err := loadTaskProjectionSnapshot(root)
	if err != nil {
		t.Fatalf("loadTaskProjectionSnapshot() error = %v", err)
	}
	if !ok {
		t.Fatal("loadTaskProjectionSnapshot() ok=false, want true")
	}
	if snap.Cursor != want {
		t.Fatalf("snapshot cursor = %#v, want %#v", snap.Cursor, want)
	}
}

func TestConsoleFileStoreMigratesLegacyTopicJSONAndTaskLog(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	logDir := filepath.Join(root, "log")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", logDir, err)
	}
	now := mustParseTime(t, "2026-03-15T10:05:00Z")
	topicsRaw, err := json.Marshal(map[string]any{
		"version":    1,
		"updated_at": now,
		"items": []TopicInfo{{
			ID:        "legacy_topic",
			Title:     "Legacy",
			CreatedAt: now,
			UpdatedAt: now,
		}, {
			ID:        "_heartbeat",
			Title:     "Heartbeat",
			CreatedAt: now,
			UpdatedAt: now,
		}},
	})
	if err != nil {
		t.Fatalf("Marshal(topic file) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "topic.json"), topicsRaw, 0o600); err != nil {
		t.Fatalf("WriteFile(topic.json) error = %v", err)
	}
	eventRaw, err := json.Marshal(map[string]any{
		"type":    "task_upsert",
		"at":      now,
		"channel": "console",
		"trigger": TaskTrigger{
			Source: "ui",
			Event:  "chat_submit",
		},
		"task": TaskInfo{
			ID:        "task_legacy",
			Status:    TaskDone,
			Task:      "legacy",
			Model:     "gpt-5.2",
			Timeout:   "10m0s",
			CreatedAt: now,
			TopicID:   "legacy_topic",
		},
	})
	if err != nil {
		t.Fatalf("Marshal(task event) error = %v", err)
	}
	heartbeatEventRaw, err := json.Marshal(map[string]any{
		"type":    "task_upsert",
		"at":      now,
		"channel": "console",
		"task": TaskInfo{
			ID:        "task_legacy_heartbeat",
			Status:    TaskDone,
			Task:      "heartbeat",
			Model:     "gpt-5.2",
			Timeout:   "10m0s",
			CreatedAt: now,
			TopicID:   "_heartbeat",
		},
	})
	if err != nil {
		t.Fatalf("Marshal(heartbeat task event) error = %v", err)
	}
	legacyLog := append(append(eventRaw, '\n'), append(heartbeatEventRaw, '\n')...)
	if err := os.WriteFile(filepath.Join(logDir, "2026-03-15_legacy_topic.jsonl"), legacyLog, 0o600); err != nil {
		t.Fatalf("WriteFile(log) error = %v", err)
	}

	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topics := store.ListTopicsPage(TopicListOptions{Limit: 10})
	if len(topics) != 2 {
		t.Fatalf("len(topics) = %d, want 2", len(topics))
	}
	topicByID := make(map[string]TopicInfo, len(topics))
	for _, topic := range topics {
		topicByID[topic.ID] = topic
	}
	if topicByID["legacy_topic"].Title != "Legacy" {
		t.Fatalf("topicByID[legacy_topic] = %#v, want Legacy", topicByID["legacy_topic"])
	}
	if topicByID[ConsoleAwarenessTopicID].Title != ConsoleAwarenessTopicTitle {
		t.Fatalf("topicByID[%s] = %#v, want awareness topic", ConsoleAwarenessTopicID, topicByID[ConsoleAwarenessTopicID])
	}
	items := store.List(TaskListOptions{Limit: 20, TopicID: "legacy_topic"})
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ID != "task_legacy" || items[0].Task != "legacy" {
		t.Fatalf("items[0] = %+v, want migrated legacy task", items[0])
	}
	trigger, ok := store.GetTrigger("task_legacy")
	if !ok {
		t.Fatalf("GetTrigger(task_legacy) ok = false, want true")
	}
	if trigger.Source != "ui" || trigger.Event != "chat_submit" {
		t.Fatalf("trigger = %+v, want migrated legacy trigger", trigger)
	}
	awarenessItems := store.List(TaskListOptions{Limit: 20, TopicID: ConsoleAwarenessTopicID})
	if len(awarenessItems) != 1 {
		t.Fatalf("len(awarenessItems) = %d, want 1", len(awarenessItems))
	}
	if awarenessItems[0].ID != "task_legacy_heartbeat" {
		t.Fatalf("awarenessItems[0].ID = %q, want task_legacy_heartbeat", awarenessItems[0].ID)
	}
	if _, err := os.Stat(filepath.Join(root, "projection.json")); err != nil {
		t.Fatalf("projection.json missing after legacy migration: %v", err)
	}

	if err := os.WriteFile(filepath.Join(logDir, "2026-03-15_legacy_topic.jsonl"), []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(corrupt legacy log) error = %v", err)
	}
	reloaded, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}
	if task, ok := reloaded.Get("task_legacy"); !ok || task == nil {
		t.Fatalf("reloaded task_legacy missing")
	}
}

func TestConsoleFileStorePersistsAwarenessTopicFromJournal(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	now := mustParseTime(t, "2026-03-15T10:05:00Z")
	if err := store.UpsertWithTrigger(TaskInfo{
		ID:        "task_awareness",
		Status:    TaskDone,
		Task:      "awareness",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: now,
		TopicID:   ConsoleAwarenessTopicID,
	}, TaskTrigger{Source: "heartbeat", Event: "heartbeat_tick"}, ""); err != nil {
		t.Fatalf("UpsertWithTrigger() error = %v", err)
	}

	reloaded, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}
	topics := reloaded.ListTopicsPage(TopicListOptions{Limit: 10})
	if len(topics) != 1 {
		t.Fatalf("len(topics) = %d, want 1", len(topics))
	}
	if topics[0].ID != ConsoleAwarenessTopicID {
		t.Fatalf("topics[0].ID = %q, want %q", topics[0].ID, ConsoleAwarenessTopicID)
	}
	if topics[0].Title != ConsoleAwarenessTopicTitle {
		t.Fatalf("topics[0].Title = %q, want %q", topics[0].Title, ConsoleAwarenessTopicTitle)
	}
	awarenessItems := reloaded.List(TaskListOptions{Limit: 20, TopicID: ConsoleAwarenessTopicID})
	if len(awarenessItems) != 1 {
		t.Fatalf("len(awarenessItems) = %d, want 1", len(awarenessItems))
	}
	if awarenessItems[0].TopicID != ConsoleAwarenessTopicID {
		t.Fatalf("awarenessItems[0].TopicID = %q, want %q", awarenessItems[0].TopicID, ConsoleAwarenessTopicID)
	}
}

func TestConsoleFileStoreTaskAndTopicUpsertIsOneCommittedEvent(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	if err := store.UpsertWithTrigger(TaskInfo{
		ID:        "task_atomic_upsert",
		Status:    TaskQueued,
		Task:      "one domain mutation",
		CreatedAt: mustParseTime(t, "2026-03-15T10:00:00Z"),
		TopicID:   "topic_atomic_upsert",
	}, TaskTrigger{Source: "ui", Event: "chat_submit"}, "Atomic topic"); err != nil {
		t.Fatalf("UpsertWithTrigger() error = %v", err)
	}

	var events []taskdomain.JournalPayload
	if err := store.journal.Replay(func(rec domainjournal.Record) error {
		if rec.Event.Domain != taskdomain.JournalDomain {
			return nil
		}
		payload, err := taskdomain.DecodeJournalPayload(rec.Event.Payload)
		if err != nil {
			return err
		}
		events = append(events, payload)
		return nil
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("committed event count = %d, want one task+topic event", len(events))
	}
	if events[0].Task == nil || events[0].Task.ID != "task_atomic_upsert" {
		t.Fatalf("event task = %#v", events[0].Task)
	}
	if events[0].Topic == nil || events[0].Topic.ID != "topic_atomic_upsert" {
		t.Fatalf("event topic = %#v", events[0].Topic)
	}
}

func TestConsoleFileStoreMutationDoesNotWriteSnapshot(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	snapshotPath := filepath.Join(root, taskProjectionSnapshotFilename)
	if err := os.Remove(snapshotPath); err != nil {
		t.Fatalf("Remove(snapshot) error = %v", err)
	}
	if err := os.Mkdir(snapshotPath, 0o700); err != nil {
		t.Fatalf("Mkdir(snapshot path) error = %v", err)
	}

	topic, err := store.CreateTopic("committed topic")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v, want committed mutation", err)
	}
	if topic.ID == "" {
		t.Fatal("CreateTopic() returned empty id")
	}
	if err := store.ProjectionError(); err != nil {
		t.Fatalf("ProjectionError() = %v, mutation should not write snapshot", err)
	}

	if err := os.Remove(snapshotPath); err != nil {
		t.Fatalf("Remove(blocking snapshot directory) error = %v", err)
	}
	reloaded, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}
	got, ok := reloaded.GetTopic(topic.ID)
	if !ok || got == nil || got.Title != "committed topic" {
		t.Fatalf("reloaded topic = %#v, ok=%v", got, ok)
	}
}

func TestConsoleFileStoreMaintainsCreatedAtOrder(t *testing.T) {
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	for _, item := range []TaskInfo{
		{ID: "middle", Status: TaskDone, CreatedAt: mustParseTime(t, "2026-03-15T10:01:00Z")},
		{ID: "oldest", Status: TaskDone, CreatedAt: mustParseTime(t, "2026-03-15T10:00:00Z")},
		{ID: "newest", Status: TaskDone, CreatedAt: mustParseTime(t, "2026-03-15T10:02:00Z")},
	} {
		if err := store.Upsert(item); err != nil {
			t.Fatalf("Upsert(%q) error = %v", item.ID, err)
		}
	}
	if got := strings.Join(store.orderedIDs, ","); got != "newest,middle,oldest" {
		t.Fatalf("ordered ids = %q", got)
	}
	page := store.List(TaskListOptions{
		Limit:  2,
		Cursor: pagination.EncodeKeysetCursor(store.items["newest"].CreatedAt, "newest"),
	})
	if len(page) != 2 || page[0].ID != "middle" || page[1].ID != "oldest" {
		t.Fatalf("cursor page = %#v", page)
	}
	if err := store.Update("oldest", func(item *TaskInfo) {
		item.CreatedAt = mustParseTime(t, "2026-03-15T10:03:00Z")
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got := strings.Join(store.orderedIDs, ","); got != "oldest,newest,middle" {
		t.Fatalf("ordered ids after update = %q", got)
	}
}

func TestConsoleFileStoreMaintainsTaskOrderByTopic(t *testing.T) {
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	for _, item := range []TaskInfo{
		{ID: "topic-a-old", TopicID: "topic-a", Status: TaskDone, CreatedAt: mustParseTime(t, "2026-03-15T10:00:00Z")},
		{ID: "topic-b", TopicID: "topic-b", Status: TaskDone, CreatedAt: mustParseTime(t, "2026-03-15T10:02:00Z")},
		{ID: "topic-a-new", TopicID: "topic-a", Status: TaskDone, CreatedAt: mustParseTime(t, "2026-03-15T10:03:00Z")},
	} {
		if err := store.Upsert(item); err != nil {
			t.Fatalf("Upsert(%q) error = %v", item.ID, err)
		}
	}
	if got := strings.Join(store.orderedIDsByTopic["topic-a"], ","); got != "topic-a-new,topic-a-old" {
		t.Fatalf("topic-a ordered ids = %q", got)
	}
	page := store.List(TaskListOptions{
		TopicID: "topic-a",
		Limit:   1,
		Cursor:  pagination.EncodeKeysetCursor(store.items["topic-a-new"].CreatedAt, "topic-a-new"),
	})
	if len(page) != 1 || page[0].ID != "topic-a-old" {
		t.Fatalf("topic cursor page = %#v", page)
	}
	if err := store.Update("topic-a-old", func(item *TaskInfo) {
		item.TopicID = "topic-b"
	}); err != nil {
		t.Fatalf("Update(topic) error = %v", err)
	}
	if got := strings.Join(store.orderedIDsByTopic["topic-a"], ","); got != "topic-a-new" {
		t.Fatalf("topic-a ordered ids after move = %q", got)
	}
	if got := strings.Join(store.orderedIDsByTopic["topic-b"], ","); got != "topic-b,topic-a-old" {
		t.Fatalf("topic-b ordered ids after move = %q", got)
	}
}

func TestConsoleFileStoreListsTopicsFromOrderedIndex(t *testing.T) {
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	for _, topic := range []TopicInfo{
		{ID: "topic_1", UpdatedAt: mustParseTime(t, "2026-03-15T10:01:00Z")},
		{ID: "topic_3", UpdatedAt: mustParseTime(t, "2026-03-15T10:03:00Z")},
		{ID: "topic_2", UpdatedAt: mustParseTime(t, "2026-03-15T10:02:00Z")},
	} {
		store.topics[topic.ID] = topic
	}
	store.orderedTopicIDs = rebuildOrderedTopicIDs(store.topics)

	first := store.ListTopicsPage(TopicListOptions{Limit: 2})
	if len(first) != 2 || first[0].ID != "topic_3" || first[1].ID != "topic_2" {
		t.Fatalf("first page = %#v", first)
	}
	second := store.ListTopicsPage(TopicListOptions{
		Limit:  2,
		Cursor: pagination.EncodeKeysetCursor(first[1].UpdatedAt, first[1].ID),
	})
	if len(second) != 1 || second[0].ID != "topic_1" {
		t.Fatalf("second page = %#v", second)
	}
}

func TestConsoleFileStoreSetTopicTitleAndDeleteTopic(t *testing.T) {
	root := t.TempDir()
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: filepath.Join(root, "journal"),
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	topic, err := store.CreateTopic("initial")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	store.UpsertWithTrigger(TaskInfo{
		ID:        "task_topic_delete",
		Status:    TaskDone,
		Task:      "hello",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: mustParseTime(t, "2026-03-15T10:02:00Z"),
		TopicID:   topic.ID,
	}, TaskTrigger{Source: "ui", Event: "chat_submit"}, "")

	if err := store.SetTopicTitle(topic.ID, "renamed"); err != nil {
		t.Fatalf("SetTopicTitle() error = %v", err)
	}
	renamed, ok := store.GetTopic(topic.ID)
	if !ok {
		t.Fatalf("topic %q not found after rename", topic.ID)
	}
	if renamed.Title != "renamed" {
		t.Fatalf("renamed.Title = %q, want renamed", renamed.Title)
	}

	deleted, err := store.DeleteTopic(topic.ID)
	if err != nil || !deleted {
		t.Fatalf("DeleteTopic(%q) = %v, %v; want true, nil", topic.ID, deleted, err)
	}
	items := store.List(TaskListOptions{Limit: 20})
	for _, item := range items {
		if item.TopicID == topic.ID {
			t.Fatalf("deleted topic task still visible: %+v", item)
		}
	}
}

func TestConsoleFileStoreDeleteTopicReturnsJournalFailure(t *testing.T) {
	root := t.TempDir()
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: filepath.Join(root, "journal"),
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topic, err := store.CreateTopic("must remain")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	if err := store.journal.Close(); err != nil {
		t.Fatalf("journal.Close() error = %v", err)
	}

	deleted, err := store.DeleteTopic(topic.ID)
	if err == nil {
		t.Fatal("DeleteTopic() error = nil, want journal append failure")
	}
	if deleted {
		t.Fatal("DeleteTopic() deleted = true after journal append failure")
	}
	if got, ok := store.GetTopic(topic.ID); !ok || got == nil {
		t.Fatal("topic missing after failed journal append")
	}
}

func TestConsoleFileStoreDoesNotWriteTopicJSON(t *testing.T) {
	root := t.TempDir()
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: filepath.Join(root, "journal"),
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	if _, err := store.CreateTopic("projected in snapshot"); err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "topic.json")); !os.IsNotExist(err) {
		t.Fatalf("topic.json stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projection.json")); err != nil {
		t.Fatalf("projection.json missing: %v", err)
	}
}

func TestConsoleFileStoreSetTopicTitleFromLLMPersistsGeneratedAt(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	topic, err := store.CreateTopic("initial")
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	if err := store.SetTopicTitleFromLLM(topic.ID, "llm title"); err != nil {
		t.Fatalf("SetTopicTitleFromLLM() error = %v", err)
	}

	updated, ok := store.GetTopic(topic.ID)
	if !ok || updated == nil {
		t.Fatalf("GetTopic(%q) missing", topic.ID)
	}
	if updated.Title != "llm title" {
		t.Fatalf("updated.Title = %q, want llm title", updated.Title)
	}
	if updated.LLMTitleGeneratedAt == nil {
		t.Fatal("updated.LLMTitleGeneratedAt = nil, want non-nil")
	}

	reloaded, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}
	reloadedTopic, ok := reloaded.GetTopic(topic.ID)
	if !ok || reloadedTopic == nil {
		t.Fatalf("reloaded topic %q missing", topic.ID)
	}
	if reloadedTopic.LLMTitleGeneratedAt == nil {
		t.Fatal("reloadedTopic.LLMTitleGeneratedAt = nil, want non-nil")
	}
}

func TestConsoleFileStoreDoesNotPrecreateDefaultTopic(t *testing.T) {
	root := t.TempDir()
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: filepath.Join(root, "journal"),
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	topics := store.ListTopicsPage(TopicListOptions{Limit: 1})
	if len(topics) != 0 {
		t.Fatalf("len(topics) = %d, want 0", len(topics))
	}
}

func TestConsoleFileStoreApplyConfigDoesNotMutateStateOnRewriteFailure(t *testing.T) {
	oldRoot := t.TempDir()
	oldJournalDir := filepath.Join(oldRoot, "journal")
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    oldRoot,
		Persist:    true,
		JournalDir: oldJournalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}

	if err := store.UpsertWithTrigger(TaskInfo{
		ID:        "task_before_apply_config_failure",
		Status:    TaskQueued,
		Task:      "hello",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: mustParseTime(t, "2026-03-15T10:03:00Z"),
		TopicID:   ConsoleDefaultTopicID,
	}, TaskTrigger{Source: "ui", Event: "chat_submit"}, ""); err != nil {
		t.Fatalf("UpsertWithTrigger() error = %v", err)
	}

	oldPersist := store.persist

	nextRoot := t.TempDir()
	blockedJournalPath := filepath.Join(nextRoot, "journal")
	if err := os.WriteFile(blockedJournalPath, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", blockedJournalPath, err)
	}

	err = store.ApplyConfig(ConsoleFileStoreOptions{
		RootDir:    nextRoot,
		Persist:    true,
		JournalDir: blockedJournalPath,
	})
	if err == nil {
		t.Fatal("ApplyConfig() error = nil, want rewrite failure")
	}

	if store.rootDir != oldRoot {
		t.Fatalf("store.rootDir = %q, want %q", store.rootDir, oldRoot)
	}
	if store.persist != oldPersist {
		t.Fatalf("store.persist = %v, want %v", store.persist, oldPersist)
	}

	if err := store.UpsertWithTrigger(TaskInfo{
		ID:        "task_after_apply_config_failure",
		Status:    TaskDone,
		Task:      "still old root",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: mustParseTime(t, "2026-03-15T10:04:00Z"),
		TopicID:   ConsoleDefaultTopicID,
	}, TaskTrigger{Source: "ui", Event: "chat_submit"}, ""); err != nil {
		t.Fatalf("UpsertWithTrigger(after failure) error = %v", err)
	}

	reloaded, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    oldRoot,
		Persist:    true,
		JournalDir: oldJournalDir,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}
	if _, ok := reloaded.Get("task_after_apply_config_failure"); !ok {
		t.Fatal("task_after_apply_config_failure missing from old root after failed ApplyConfig")
	}
}

func TestConsoleFileStoreApplyConfigSeedsNewJournal(t *testing.T) {
	oldRoot := t.TempDir()
	oldJournalDir := filepath.Join(oldRoot, "journal")
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    oldRoot,
		Persist:    true,
		JournalDir: oldJournalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	now := mustParseTime(t, "2026-03-15T10:06:00Z")
	if err := store.UpsertWithTrigger(TaskInfo{
		ID:        "task_seed_new_journal",
		Status:    TaskDone,
		Task:      "persist me",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: now,
		TopicID:   "topic_seed",
	}, TaskTrigger{Source: "ui", Event: "chat_submit", TraceID: "trace_seed"}, "Seed Topic"); err != nil {
		t.Fatalf("UpsertWithTrigger() error = %v", err)
	}

	nextRoot := t.TempDir()
	nextJournalDir := filepath.Join(nextRoot, "journal")
	if err := store.ApplyConfig(ConsoleFileStoreOptions{
		RootDir:    nextRoot,
		Persist:    true,
		JournalDir: nextJournalDir,
	}); err != nil {
		t.Fatalf("ApplyConfig() error = %v", err)
	}

	snap, ok, err := loadTaskProjectionSnapshot(nextRoot)
	if err != nil {
		t.Fatalf("loadTaskProjectionSnapshot(nextRoot) error = %v", err)
	}
	if !ok {
		t.Fatal("next projection snapshot missing")
	}
	if snap.Cursor.File == "" {
		t.Fatalf("next snapshot cursor = %#v, want cursor in new journal", snap.Cursor)
	}
	if _, err := os.Stat(filepath.Join(nextJournalDir, snap.Cursor.File)); err != nil {
		t.Fatalf("new journal cursor file missing: %v", err)
	}

	reloaded, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    nextRoot,
		Persist:    true,
		JournalDir: nextJournalDir,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore(nextRoot) error = %v", err)
	}
	task, ok := reloaded.Get("task_seed_new_journal")
	if !ok || task == nil {
		t.Fatal("task_seed_new_journal missing after reload")
	}
	if task.TopicID != "topic_seed" {
		t.Fatalf("task.TopicID = %q, want topic_seed", task.TopicID)
	}
	trigger, ok := reloaded.GetTrigger("task_seed_new_journal")
	if !ok {
		t.Fatal("task_seed_new_journal trigger missing after reload")
	}
	if trigger.TraceID != "trace_seed" {
		t.Fatalf("trigger.TraceID = %q, want trace_seed", trigger.TraceID)
	}
}

func TestConsoleFileStoreApplyConfigEnablesPersistenceWithoutDroppingMemoryState(t *testing.T) {
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		Persist: false,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	now := mustParseTime(t, "2026-03-15T10:07:00Z")
	if err := store.UpsertWithTrigger(TaskInfo{
		ID:        "task_enable_persist",
		Status:    TaskDone,
		Task:      "save me",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: now,
		TopicID:   "topic_enable",
	}, TaskTrigger{Source: "ui", Event: "chat_submit"}, "Enable Topic"); err != nil {
		t.Fatalf("UpsertWithTrigger() error = %v", err)
	}

	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	if err := store.ApplyConfig(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	}); err != nil {
		t.Fatalf("ApplyConfig(enable persist) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projection.json")); err != nil {
		t.Fatalf("projection.json missing after enabling persistence: %v", err)
	}

	reloaded, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}
	if task, ok := reloaded.Get("task_enable_persist"); !ok || task == nil {
		t.Fatal("task_enable_persist missing after reload")
	}
	if topic, ok := reloaded.GetTopic("topic_enable"); !ok || topic == nil || topic.Title != "Enable Topic" {
		t.Fatalf("topic_enable = %#v, ok=%v; want persisted topic", topic, ok)
	}
}

func TestConsoleFileStoreRequiresJournalDirWhenPersistent(t *testing.T) {
	_, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir: t.TempDir(),
		Persist: true,
	})
	if err == nil {
		t.Fatal("NewConsoleFileStore() error = nil, want journal dir error")
	}
	if !strings.Contains(err.Error(), "journal dir is required") {
		t.Fatalf("NewConsoleFileStore() error = %v, want journal dir error", err)
	}
}

func TestConsoleFileStoreLoadsSnapshotAndSkipsJournalBeforeCursor(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(journalDir) error = %v", err)
	}
	badLine := []byte("{bad json}\n")
	if err := os.WriteFile(filepath.Join(journalDir, "events.000000000000000001.jsonl"), badLine, 0o600); err != nil {
		t.Fatalf("WriteFile(journal) error = %v", err)
	}
	now := mustParseTime(t, "2026-03-15T10:00:00Z")
	writeTaskProjectionSnapshotFixture(t, root, map[string]any{
		"version":    1,
		"updated_at": "2026-03-15T10:00:00Z",
		"cursor": map[string]any{
			"file": "events.000000000000000001.jsonl",
			"line": 1,
			"byte": len(badLine),
		},
		"items": []TaskInfo{{
			ID:        "task_snapshot",
			Status:    TaskDone,
			Task:      "from snapshot",
			Model:     "gpt-5.2",
			Timeout:   "10m0s",
			CreatedAt: now,
			TopicID:   "topic_snapshot",
		}},
		"topics": []TopicInfo{{
			ID:        "topic_snapshot",
			Title:     "Snapshot Topic",
			CreatedAt: now,
			UpdatedAt: now,
		}},
		"triggers": map[string]TaskTrigger{
			"task_snapshot": {Source: "ui", Event: "chat_submit"},
		},
	})

	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:    root,
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	task, ok := store.Get("task_snapshot")
	if !ok || task == nil {
		t.Fatal("task_snapshot missing")
	}
	if task.TopicID != "topic_snapshot" {
		t.Fatalf("task.TopicID = %q, want topic_snapshot", task.TopicID)
	}
	topic, ok := store.GetTopic("topic_snapshot")
	if !ok || topic == nil {
		t.Fatal("topic_snapshot missing")
	}
	if topic.Title != "Snapshot Topic" {
		t.Fatalf("topic.Title = %q, want Snapshot Topic", topic.Title)
	}
}

func mustParseTime(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("time.Parse(%q) error = %v", raw, err)
	}
	return parsed.UTC()
}
