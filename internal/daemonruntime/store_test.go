package daemonruntime

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pagination"
)

func TestMemoryStoreUpsertListGetUpdate(t *testing.T) {
	t.Parallel()

	s := NewMemoryStore(100)
	createdAt := time.Now().UTC().Add(-1 * time.Minute)
	s.Upsert(TaskInfo{
		ID:        "tg_1_1",
		Status:    TaskQueued,
		Task:      "hello",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: createdAt,
	})

	items := s.List(TaskListOptions{Limit: 20})
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Status != TaskQueued {
		t.Fatalf("status = %q, want %q", items[0].Status, TaskQueued)
	}

	s.Update("tg_1_1", func(info *TaskInfo) {
		now := time.Now().UTC()
		info.Status = TaskRunning
		info.StartedAt = &now
	})
	s.Update("tg_1_1", func(info *TaskInfo) {
		now := time.Now().UTC()
		info.Status = TaskDone
		info.FinishedAt = &now
	})

	item, ok := s.Get("tg_1_1")
	if !ok || item == nil {
		t.Fatalf("Get() not found")
	}
	if item.Status != TaskDone {
		t.Fatalf("status = %q, want %q", item.Status, TaskDone)
	}
	if item.StartedAt == nil || item.FinishedAt == nil {
		t.Fatalf("expected started/finished timestamps")
	}
}

func TestMemoryStoreListCursor(t *testing.T) {
	t.Parallel()

	s := NewMemoryStore(100)
	base := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"task_c", "task_b", "task_a"} {
		s.Upsert(TaskInfo{
			ID:        id,
			Status:    TaskDone,
			Task:      id,
			Model:     "gpt-5.2",
			Timeout:   "10m0s",
			CreatedAt: base.Add(time.Duration(-i) * time.Minute),
		})
	}

	firstPage := s.List(TaskListOptions{Limit: 2})
	if len(firstPage) != 2 {
		t.Fatalf("len(firstPage) = %d, want 2", len(firstPage))
	}
	if firstPage[0].ID != "task_c" || firstPage[1].ID != "task_b" {
		t.Fatalf("firstPage = %+v, want [task_c task_b]", firstPage)
	}

	secondPage := s.List(TaskListOptions{
		Limit:  2,
		Cursor: pagination.EncodeKeysetCursor(firstPage[1].CreatedAt, firstPage[1].ID),
	})
	if len(secondPage) != 1 {
		t.Fatalf("len(secondPage) = %d, want 1", len(secondPage))
	}
	if secondPage[0].ID != "task_a" {
		t.Fatalf("secondPage[0].ID = %q, want task_a", secondPage[0].ID)
	}
}

func TestBuildTaskID(t *testing.T) {
	t.Parallel()
	got := BuildTaskID("sl", "T-1", "C/2", "1740130000.123")
	if got != "sl_T-1_C_2_1740130000_123" {
		t.Fatalf("BuildTaskID() = %q", got)
	}
}
