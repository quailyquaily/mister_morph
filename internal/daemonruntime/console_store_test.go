package daemonruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	store.UpsertWithTrigger(TaskInfo{
		ID:        "task_default",
		Status:    TaskQueued,
		Task:      "hello",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: mustParseTime(t, "2026-03-15T10:00:00Z"),
		TopicID:   ConsoleDefaultTopicID,
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

	topics := reloaded.ListTopics()
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

func TestConsoleFileStoreDoesNotReadLegacyTaskLog(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(logDir, "2026-03-15_legacy_topic.jsonl"), append(eventRaw, '\n'), 0o600); err != nil {
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
	if topics := store.ListTopics(); len(topics) != 0 {
		t.Fatalf("len(topics) = %d, want 0; old topic.json must not be read", len(topics))
	}
	if items := store.List(TaskListOptions{Limit: 20, TopicID: "legacy_topic"}); len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0; old task log must not be read", len(items))
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
	topics := reloaded.ListTopics()
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
	topics := store.ListTopics()
	foundRenamed := false
	for _, item := range topics {
		if item.ID != topic.ID {
			continue
		}
		foundRenamed = true
		if item.Title != "renamed" {
			t.Fatalf("item.Title = %q, want renamed", item.Title)
		}
	}
	if !foundRenamed {
		t.Fatalf("topic %q not found after rename", topic.ID)
	}

	if !store.DeleteTopic(topic.ID) {
		t.Fatalf("DeleteTopic(%q) = false, want true", topic.ID)
	}
	items := store.List(TaskListOptions{Limit: 20})
	for _, item := range items {
		if item.TopicID == topic.ID {
			t.Fatalf("deleted topic task still visible: %+v", item)
		}
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

	topics := store.ListTopics()
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
