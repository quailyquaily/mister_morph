package daemonruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/domainjournal"
)

func TestFileTaskStoreReplayAndRecover(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")

	store, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:        root,
		Target:         "telegram",
		Persist:        true,
		RotateMaxBytes: 1 << 20,
		JournalDir:     journalDir,
	})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
	}

	store.Upsert(TaskInfo{
		ID:        "task_running",
		Status:    TaskRunning,
		Task:      "hello",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: mustParseTime(t, "2026-03-15T10:00:00Z"),
	})

	reloaded, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:        root,
		Target:         "telegram",
		Persist:        true,
		RotateMaxBytes: 1 << 20,
		JournalDir:     journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewFileTaskStore() error = %v", err)
	}

	task, ok := reloaded.Get("task_running")
	if !ok || task == nil {
		t.Fatalf("reloaded task missing")
	}
	if task.Status != TaskCanceled {
		t.Fatalf("task.Status = %q, want %q", task.Status, TaskCanceled)
	}
	if task.Error != "runtime restarted" {
		t.Fatalf("task.Error = %q, want runtime restarted", task.Error)
	}
}

func TestFileTaskStoreRotatesAndReplays(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")

	store, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:        root,
		Target:         "slack",
		Persist:        true,
		RotateMaxBytes: 180,
		JournalDir:     journalDir,
	})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
	}

	for i := 0; i < 3; i++ {
		store.Upsert(TaskInfo{
			ID:        BuildTaskID("task", i),
			Status:    TaskDone,
			Task:      strings.Repeat("rotate ", 20),
			Model:     "gpt-5.2",
			Timeout:   "10m0s",
			CreatedAt: time.Date(2026, 3, 15, 10, i, 0, 0, time.UTC),
		})
	}

	entries, err := os.ReadDir(journalDir)
	if err != nil {
		t.Fatalf("ReadDir(journal) error = %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("len(entries) = %d, want at least 2 rotated files", len(entries))
	}

	reloaded, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:        root,
		Target:         "slack",
		Persist:        true,
		RotateMaxBytes: 180,
		JournalDir:     journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewFileTaskStore() error = %v", err)
	}
	items := reloaded.List(TaskListOptions{Limit: 10})
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(items))
	}
}

func TestFileTaskStoreDoesNotMutateWhenJournalAppendFails(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	store, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:    root,
		Target:     "slack",
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
	}
	if err := store.journal.Close(); err != nil {
		t.Fatalf("journal.Close() error = %v", err)
	}

	err = store.RecordTaskUpsert(TaskInfo{
		ID:        "task_append_fail",
		Status:    TaskDone,
		Task:      "must not be visible",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: mustParseTime(t, "2026-03-15T10:00:00Z"),
	}, TaskTrigger{})
	if err == nil {
		t.Fatal("RecordTaskUpsert() error = nil, want append failure")
	}
	if _, ok := store.Get("task_append_fail"); ok {
		t.Fatalf("task exists after failed journal append")
	}
}

func TestFileTaskStoreRequiresJournalDirWhenPersistent(t *testing.T) {
	_, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir: t.TempDir(),
		Target:  "slack",
		Persist: true,
	})
	if err == nil {
		t.Fatal("NewFileTaskStore() error = nil, want journal dir error")
	}
	if !strings.Contains(err.Error(), "journal dir is required") {
		t.Fatalf("NewFileTaskStore() error = %v, want journal dir error", err)
	}
}

func TestFileTaskStoreWritesUpsertAndUpdateTypesForTerminalStatuses(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	store, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:    root,
		Target:     "slack",
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
	}

	if err := store.RecordTaskUpsert(TaskInfo{
		ID:        "task_terminal",
		Status:    TaskDone,
		Task:      "done task",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: mustParseTime(t, "2026-03-15T10:00:00Z"),
	}, TaskTrigger{}); err != nil {
		t.Fatalf("RecordTaskUpsert() error = %v", err)
	}
	if err := store.RecordTaskUpdate("task_terminal", TaskTrigger{}, func(info *TaskInfo) {
		info.Status = TaskFailed
		info.Error = "failed"
	}); err != nil {
		t.Fatalf("RecordTaskUpdate() error = %v", err)
	}

	var types []string
	if err := domainjournal.ReplayDir(journalDir, func(rec domainjournal.Record) error {
		if rec.Event.Domain == taskJournalDomain {
			types = append(types, rec.Event.Type)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	want := []string{taskJournalTypeTaskUpsert, taskJournalTypeTaskUpdate}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("journal event types = %#v, want %#v", types, want)
	}
}

func TestFileTaskStoreLoadsSnapshotAndSkipsJournalBeforeCursor(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(journalDir) error = %v", err)
	}
	badLine := []byte("{bad json}\n")
	if err := os.WriteFile(filepath.Join(journalDir, "events.000000000000000001.jsonl"), badLine, 0o600); err != nil {
		t.Fatalf("WriteFile(journal) error = %v", err)
	}
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
			CreatedAt: mustParseTime(t, "2026-03-15T10:00:00Z"),
		}},
		"triggers": map[string]TaskTrigger{
			"task_snapshot": {Source: "ui", Event: "chat_submit"},
		},
	})

	store, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:    root,
		Target:     "slack",
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
	}
	task, ok := store.Get("task_snapshot")
	if !ok || task == nil {
		t.Fatal("task_snapshot missing")
	}
	if task.Task != "from snapshot" {
		t.Fatalf("task.Task = %q, want from snapshot", task.Task)
	}
	trigger, ok := store.triggers["task_snapshot"]
	if !ok || trigger.Source != "ui" {
		t.Fatalf("trigger = %#v, ok=%v, want snapshot trigger", trigger, ok)
	}
}

func writeTaskProjectionSnapshotFixture(t *testing.T, root string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal(snapshot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "projection.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile(snapshot) error = %v", err)
	}
}
