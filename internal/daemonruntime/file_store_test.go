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

func TestNewTaskViewForTargetUsesExplicitPersistenceConfig(t *testing.T) {
	root := t.TempDir()
	tasksDir := filepath.Join(root, "runtime-tasks")
	journalDir := filepath.Join(root, "runtime-journal")

	view, err := NewTaskViewForTarget("telegram", 10, TaskViewConfig{
		PersistenceTargets: []string{"telegram"},
		TasksDir:           tasksDir,
		JournalDir:         journalDir,
		RotateMaxBytes:     4096,
	})
	if err != nil {
		t.Fatalf("NewTaskViewForTarget() error = %v", err)
	}
	store, ok := view.(*FileTaskStore)
	if !ok {
		t.Fatalf("task view type = %T, want *FileTaskStore", view)
	}
	if got, want := store.rootDir, filepath.Join(tasksDir, "telegram"); got != want {
		t.Fatalf("rootDir = %q, want %q", got, want)
	}
	if got := store.rotateMaxBytes; got != 4096 {
		t.Fatalf("rotateMaxBytes = %d, want 4096", got)
	}
	if info, statErr := os.Stat(journalDir); statErr != nil || !info.IsDir() {
		t.Fatalf("journal directory was not created at %q: %v", journalDir, statErr)
	}
}

func TestNewTaskViewForTargetJournalsWithoutPersistingProjection(t *testing.T) {
	root := t.TempDir()
	tasksDir := filepath.Join(root, "tasks")
	journalDir := filepath.Join(root, "journal")
	config := TaskViewConfig{
		PersistenceTargets: []string{"telegram"},
		TasksDir:           tasksDir,
		JournalDir:         journalDir,
	}
	view, err := NewTaskViewForTarget("slack", 2, config)
	if err != nil {
		t.Fatalf("NewTaskViewForTarget() error = %v", err)
	}
	store, ok := view.(*FileTaskStore)
	if !ok {
		t.Fatalf("task view type = %T, want *FileTaskStore", view)
	}
	for i, id := range []string{"oldest", "middle", "newest"} {
		if err := store.Upsert(TaskInfo{
			ID:        id,
			Status:    TaskDone,
			CreatedAt: time.Date(2026, 8, 19, 12, i, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("Upsert(%q) error = %v", id, err)
		}
	}
	items := store.List(TaskListOptions{Limit: 10})
	if len(items) != 2 || items[0].ID != "newest" || items[1].ID != "middle" {
		t.Fatalf("live items = %#v, want newest and middle", items)
	}
	if _, err := os.Stat(filepath.Join(tasksDir, "slack", "projection.json")); !os.IsNotExist(err) {
		t.Fatalf("projection stat error = %v, want not exist", err)
	}
	if err := store.journal.Close(); err != nil {
		t.Fatalf("journal.Close() error = %v", err)
	}

	eventCount := 0
	if err := domainjournal.ReplayDir(journalDir, func(rec domainjournal.Record) error {
		if rec.Event.Domain == taskdomain.JournalDomain && rec.Event.Trace.Target == "slack" {
			eventCount++
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	if eventCount != 3 {
		t.Fatalf("task event count = %d, want 3", eventCount)
	}

	reloaded, err := NewTaskViewForTarget("slack", 2, config)
	if err != nil {
		t.Fatalf("reload NewTaskViewForTarget() error = %v", err)
	}
	if got := reloaded.List(TaskListOptions{Limit: 10}); len(got) != 0 {
		t.Fatalf("reloaded items = %#v, want no replay", got)
	}
}

func TestFileTaskStoreKeepsActiveTasksOutsideTerminalHistoryLimit(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	store, err := NewFileTaskStore(FileTaskStoreOptions{
		Target:     "slack",
		Persist:    false,
		MaxItems:   1,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
	}
	if err := store.Upsert(TaskInfo{
		ID:        "task_active",
		Status:    TaskRunning,
		CreatedAt: mustParseTime(t, "2026-08-19T10:00:00Z"),
	}); err != nil {
		t.Fatalf("Upsert(active) error = %v", err)
	}
	if err := store.Upsert(TaskInfo{
		ID:        "task_recent_done",
		Status:    TaskDone,
		CreatedAt: mustParseTime(t, "2026-08-19T10:01:00Z"),
	}); err != nil {
		t.Fatalf("Upsert(done) error = %v", err)
	}

	items := store.List(TaskListOptions{Limit: 10})
	if len(items) != 2 || items[0].ID != "task_recent_done" || items[1].ID != "task_active" {
		t.Fatalf("live items = %#v, want recent terminal history plus active task", items)
	}
	if err := store.Update("task_active", func(info *TaskInfo) {
		info.Status = TaskDone
	}); err != nil {
		t.Fatalf("Update(active) error = %v", err)
	}
	items = store.List(TaskListOptions{Limit: 10})
	if len(items) != 1 || items[0].ID != "task_recent_done" {
		t.Fatalf("live items after completion = %#v, want newest terminal task", items)
	}

	var activeEventTypes []string
	if err := domainjournal.ReplayDir(journalDir, func(rec domainjournal.Record) error {
		if rec.Event.Domain == taskdomain.JournalDomain && rec.Event.Trace.TaskID == "task_active" {
			activeEventTypes = append(activeEventTypes, rec.Event.Type)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	wantEventTypes := []string{taskdomain.JournalTypeTaskUpsert, taskdomain.JournalTypeTaskUpdate}
	if strings.Join(activeEventTypes, ",") != strings.Join(wantEventTypes, ",") {
		t.Fatalf("active task event types = %#v, want %#v", activeEventTypes, wantEventTypes)
	}
}

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

	pendingAt := mustParseTime(t, "2026-03-15T10:01:00Z")
	store.Upsert(TaskInfo{
		ID:                "task_pending",
		Status:            TaskPending,
		Task:              "hello",
		Model:             "gpt-5.2",
		Timeout:           "10m0s",
		CreatedAt:         mustParseTime(t, "2026-03-15T10:00:00Z"),
		PendingAt:         &pendingAt,
		ApprovalRequestID: "apr_restart",
		Result:            map[string]any{"pending": true},
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

	task, ok := reloaded.Get("task_pending")
	if !ok || task == nil {
		t.Fatalf("reloaded task missing")
	}
	if task.Status != TaskCanceled {
		t.Fatalf("task.Status = %q, want %q", task.Status, TaskCanceled)
	}
	if task.Error != "runtime restarted" {
		t.Fatalf("task.Error = %q, want runtime restarted", task.Error)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared after restart", task.PendingAt, task.ApprovalRequestID, task.Result)
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

func TestFileTaskStoreUpsertReturnsJournalAppendFailure(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:    root,
		Target:     "slack",
		Persist:    true,
		JournalDir: filepath.Join(root, "journal"),
	})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
	}
	if err := store.journal.Close(); err != nil {
		t.Fatalf("journal.Close() error = %v", err)
	}

	err = store.Upsert(TaskInfo{ID: "task_direct_upsert", Status: TaskQueued})
	if err == nil {
		t.Fatal("Upsert() error = nil, want journal append failure")
	}
	if _, ok := store.Get("task_direct_upsert"); ok {
		t.Fatal("task exists after failed direct Upsert")
	}
}

func TestFileTaskStoreMutationDoesNotWriteSnapshot(t *testing.T) {
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

	snapshotPath := filepath.Join(root, taskProjectionSnapshotFilename)
	if err := os.Remove(snapshotPath); err != nil {
		t.Fatalf("Remove(snapshot) error = %v", err)
	}
	if err := os.Mkdir(snapshotPath, 0o700); err != nil {
		t.Fatalf("Mkdir(snapshot path) error = %v", err)
	}

	err = store.RecordTaskUpsert(TaskInfo{
		ID:        "task_snapshot_deferred",
		Status:    TaskDone,
		Task:      "committed in journal",
		CreatedAt: mustParseTime(t, "2026-03-15T10:00:00Z"),
	}, TaskTrigger{})
	if err != nil {
		t.Fatalf("RecordTaskUpsert() error = %v, want committed mutation", err)
	}
	if task, ok := store.Get("task_snapshot_deferred"); !ok || task == nil {
		t.Fatal("committed task missing from live projection")
	}
	if err := store.ProjectionError(); err != nil {
		t.Fatalf("ProjectionError() = %v, mutation should not write snapshot", err)
	}

	if err := os.Remove(snapshotPath); err != nil {
		t.Fatalf("Remove(blocking snapshot directory) error = %v", err)
	}
	reloaded, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:    root,
		Target:     "slack",
		Persist:    true,
		JournalDir: journalDir,
	})
	if err != nil {
		t.Fatalf("reload NewFileTaskStore() error = %v", err)
	}
	items := reloaded.List(TaskListOptions{Limit: 10})
	if len(items) != 1 || items[0].ID != "task_snapshot_deferred" {
		t.Fatalf("reloaded items = %#v, want one committed task", items)
	}

	eventCount := 0
	if err := domainjournal.ReplayDir(journalDir, func(rec domainjournal.Record) error {
		if rec.Event.Domain == taskdomain.JournalDomain && rec.Event.Trace.TaskID == "task_snapshot_deferred" {
			eventCount++
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("committed task event count = %d, want 1", eventCount)
	}
}

func TestFileTaskStoreMaintainsCreatedAtOrder(t *testing.T) {
	store, err := NewFileTaskStore(FileTaskStoreOptions{Target: "slack"})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
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
		if rec.Event.Domain == taskdomain.JournalDomain {
			types = append(types, rec.Event.Type)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplayDir() error = %v", err)
	}
	want := []string{taskdomain.JournalTypeTaskUpsert, taskdomain.JournalTypeTaskUpdate}
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

func TestFileTaskStoreSnapshotAdvancesPastForeignDomainEvents(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	raw, err := domainjournal.New(domainjournal.JournalOptions{Dir: journalDir})
	if err != nil {
		t.Fatalf("domainjournal.New() error = %v", err)
	}
	want, err := raw.Append(domainjournal.Event{
		ID:            "evt_conversation",
		Time:          "2026-03-15T10:00:00Z",
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

	if _, err := NewFileTaskStore(FileTaskStoreOptions{
		RootDir:    root,
		Target:     "telegram",
		Persist:    true,
		JournalDir: journalDir,
	}); err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
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
