package daemonruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileTaskStoreReplayAndRecover(t *testing.T) {
	root := t.TempDir()

	store, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:        root,
		Target:         "telegram",
		Persist:        true,
		RotateMaxBytes: 1 << 20,
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

	store, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:        root,
		Target:         "slack",
		Persist:        true,
		RotateMaxBytes: 180,
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

	entries, err := os.ReadDir(filepath.Join(root, "log"))
	if err != nil {
		t.Fatalf("ReadDir(log) error = %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("len(entries) = %d, want at least 2 rotated files", len(entries))
	}

	reloaded, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:        root,
		Target:         "slack",
		Persist:        true,
		RotateMaxBytes: 180,
	})
	if err != nil {
		t.Fatalf("reload NewFileTaskStore() error = %v", err)
	}
	items := reloaded.List(TaskListOptions{Limit: 10})
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(items))
	}
}
