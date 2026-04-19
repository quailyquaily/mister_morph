package daemonruntime

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConsoleFileStoreReplayAndHeartbeatFiltering(t *testing.T) {
	root := t.TempDir()

	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:          root,
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
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
		ID:        "task_heartbeat",
		Status:    TaskQueued,
		Task:      "heartbeat",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: mustParseTime(t, "2026-03-15T10:01:00Z"),
		TopicID:   "_heartbeat",
	}, TaskTrigger{Source: "heartbeat", Event: "heartbeat_tick"}, ConsoleHeartbeatTopicTitle)

	visible := store.List(TaskListOptions{Limit: 20})
	if len(visible) != 1 {
		t.Fatalf("len(visible) = %d, want 1", len(visible))
	}
	if visible[0].ID != "task_default" {
		t.Fatalf("visible[0].ID = %q, want task_default", visible[0].ID)
	}

	heartbeatItems := store.List(TaskListOptions{Limit: 20, TopicID: "_heartbeat"})
	if len(heartbeatItems) != 1 {
		t.Fatalf("len(heartbeatItems) = %d, want 1", len(heartbeatItems))
	}
	if heartbeatItems[0].ID != "task_heartbeat" {
		t.Fatalf("heartbeatItems[0].ID = %q, want task_heartbeat", heartbeatItems[0].ID)
	}

	reloaded, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:          root,
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
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
	if topics[0].ID != "_heartbeat" {
		t.Fatalf("topics[0].ID = %q, want _heartbeat", topics[0].ID)
	}
}

func TestConsoleFileStoreSetTopicTitleAndDeleteTopic(t *testing.T) {
	store, err := NewConsoleFileStore(ConsoleFileStoreOptions{
		RootDir:          t.TempDir(),
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
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
		RootDir:          root,
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
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
		RootDir:          root,
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
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
		RootDir:          t.TempDir(),
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
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
		RootDir:          oldRoot,
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
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
	oldHeartbeatTopicID := store.heartbeatTopicID
	oldPersist := store.persist

	nextRoot := t.TempDir()
	blockedLogPath := filepath.Join(nextRoot, "log")
	if err := os.WriteFile(blockedLogPath, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", blockedLogPath, err)
	}

	err = store.ApplyConfig(ConsoleFileStoreOptions{
		RootDir:          nextRoot,
		HeartbeatTopicID: "_heartbeat_next",
		Persist:          true,
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
	if store.heartbeatTopicID != oldHeartbeatTopicID {
		t.Fatalf("store.heartbeatTopicID = %q, want %q", store.heartbeatTopicID, oldHeartbeatTopicID)
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
		RootDir:          oldRoot,
		HeartbeatTopicID: "_heartbeat",
		Persist:          true,
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
