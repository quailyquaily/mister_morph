package daemonruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConsoleFileStoreReplayAndAwarenessFiltering(t *testing.T) {
	root := t.TempDir()

	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir: root,
		Persist: true,
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
		RootDir: root,
		Persist: true,
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

func TestConsoleFileStoreMigratesLegacyHeartbeatTopic(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "log")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", logDir, err)
	}
	now := mustParseTime(t, "2026-03-15T10:05:00Z")
	topicsRaw, err := json.Marshal(consoleTopicFile{
		Version:   consoleTopicFileVersion,
		UpdatedAt: now,
		Items: []TopicInfo{{
			ID:        consoleLegacyHeartbeatTopicID,
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
	eventRaw, err := json.Marshal(consoleTaskEvent{
		Type:    consoleTaskEventTypeUpsert,
		At:      now,
		Channel: "console",
		Task: TaskInfo{
			ID:        "task_legacy_awareness",
			Status:    TaskDone,
			Task:      "legacy heartbeat",
			Model:     "gpt-5.2",
			Timeout:   "10m0s",
			CreatedAt: now,
			TopicID:   consoleLegacyHeartbeatTopicID,
		},
	})
	if err != nil {
		t.Fatalf("Marshal(task event) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "2026-03-15__heartbeat.jsonl"), append(eventRaw, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile(log) error = %v", err)
	}

	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir: root,
		Persist: true,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	topics := store.ListTopics()
	if len(topics) != 1 {
		t.Fatalf("len(topics) = %d, want 1", len(topics))
	}
	if topics[0].ID != ConsoleAwarenessTopicID {
		t.Fatalf("topics[0].ID = %q, want %q", topics[0].ID, ConsoleAwarenessTopicID)
	}
	if topics[0].Title != ConsoleAwarenessTopicTitle {
		t.Fatalf("topics[0].Title = %q, want %q", topics[0].Title, ConsoleAwarenessTopicTitle)
	}
	visible := store.List(TaskListOptions{Limit: 20})
	if len(visible) != 0 {
		t.Fatalf("len(visible) = %d, want 0", len(visible))
	}
	awarenessItems := store.List(TaskListOptions{Limit: 20, TopicID: ConsoleAwarenessTopicID})
	if len(awarenessItems) != 1 {
		t.Fatalf("len(awarenessItems) = %d, want 1", len(awarenessItems))
	}
	if awarenessItems[0].TopicID != ConsoleAwarenessTopicID {
		t.Fatalf("awarenessItems[0].TopicID = %q, want %q", awarenessItems[0].TopicID, ConsoleAwarenessTopicID)
	}
	legacyItems := store.List(TaskListOptions{Limit: 20, TopicID: consoleLegacyHeartbeatTopicID})
	if len(legacyItems) != 1 {
		t.Fatalf("len(legacyItems) = %d, want 1", len(legacyItems))
	}
}

func TestConsoleFileStoreSetTopicTitleAndDeleteTopic(t *testing.T) {
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir: t.TempDir(),
		Persist: true,
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

func TestConsoleFileStoreSetTopicTitleFromLLMPersistsGeneratedAt(t *testing.T) {
	root := t.TempDir()
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir: root,
		Persist: true,
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
		RootDir: root,
		Persist: true,
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
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir: t.TempDir(),
		Persist: true,
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
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir: oldRoot,
		Persist: true,
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

	oldLogDir := store.logDir
	oldTopicPath := store.topicPath
	oldPersist := store.persist

	nextRoot := t.TempDir()
	blockedLogPath := filepath.Join(nextRoot, "log")
	if err := os.WriteFile(blockedLogPath, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", blockedLogPath, err)
	}

	err = store.ApplyConfig(ConsoleFileStoreOptions{
		RootDir: nextRoot,
		Persist: true,
	})
	if err == nil {
		t.Fatal("ApplyConfig() error = nil, want rewrite failure")
	}

	if store.rootDir != oldRoot {
		t.Fatalf("store.rootDir = %q, want %q", store.rootDir, oldRoot)
	}
	if store.logDir != oldLogDir {
		t.Fatalf("store.logDir = %q, want %q", store.logDir, oldLogDir)
	}
	if store.topicPath != oldTopicPath {
		t.Fatalf("store.topicPath = %q, want %q", store.topicPath, oldTopicPath)
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
		RootDir: oldRoot,
		Persist: true,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}
	if _, ok := reloaded.Get("task_after_apply_config_failure"); !ok {
		t.Fatal("task_after_apply_config_failure missing from old root after failed ApplyConfig")
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
