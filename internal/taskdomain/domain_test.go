package taskdomain

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingStore struct {
	upserted TaskInfo
	updated  string
	err      error
}

func TestEndedByCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !EndedByCancellation(ctx, errors.New("provider stopped")) {
		t.Fatal("EndedByCancellation() = false for canceled context")
	}
	if !EndedByCancellation(context.Background(), context.DeadlineExceeded) {
		t.Fatal("EndedByCancellation() = false for deadline error")
	}
	if EndedByCancellation(context.Background(), errors.New("model failed")) {
		t.Fatal("EndedByCancellation() = true for ordinary error")
	}
	if EndedByCancellation(ctx, nil) {
		t.Fatal("EndedByCancellation() = true for nil error")
	}
}

func (s *recordingStore) Upsert(info TaskInfo) error {
	s.upserted = info
	return s.err
}

func (s *recordingStore) Update(id string, fn func(*TaskInfo)) error {
	s.updated = id
	info := TaskInfo{ID: id}
	fn(&info)
	s.upserted = info
	return s.err
}

type recordingEventStore struct {
	recordingStore
	trigger TaskTrigger
	event   string
}

func (s *recordingEventStore) RecordTaskUpsert(info TaskInfo, trigger TaskTrigger) error {
	s.event = "upsert"
	s.upserted = info
	s.trigger = trigger
	return s.err
}

func (s *recordingEventStore) RecordTaskUpdate(id string, trigger TaskTrigger, fn func(*TaskInfo)) error {
	s.event = "update"
	s.updated = id
	s.trigger = trigger
	info := TaskInfo{ID: id}
	fn(&info)
	s.upserted = info
	return s.err
}

func TestParseTaskStatus(t *testing.T) {
	status, ok := ParseTaskStatus(" RUNNING ")
	if !ok || status != TaskRunning {
		t.Fatalf("ParseTaskStatus() = %q, %v, want running, true", status, ok)
	}
	if _, ok := ParseTaskStatus("unknown"); ok {
		t.Fatal("ParseTaskStatus(unknown) ok = true, want false")
	}
}

func TestRecordTaskMutationUsesEventRecorderAndPropagatesErrors(t *testing.T) {
	wantErr := errors.New("journal failed")
	store := &recordingEventStore{recordingStore: recordingStore{err: wantErr}}
	trigger := TaskTrigger{Source: "integration", TraceID: "trace_1"}
	info := TaskInfo{ID: "task_1", Status: TaskQueued, CreatedAt: time.Now()}

	if err := RecordTaskUpsert(store, info, trigger); !errors.Is(err, wantErr) {
		t.Fatalf("RecordTaskUpsert() error = %v, want %v", err, wantErr)
	}
	if store.event != "upsert" || store.upserted.ID != "task_1" || store.trigger != trigger {
		t.Fatalf("upsert event = %q, info = %+v, trigger = %+v", store.event, store.upserted, store.trigger)
	}

	store.err = nil
	if err := RecordTaskUpdate(store, "task_1", trigger, func(task *TaskInfo) {
		task.Status = TaskDone
	}); err != nil {
		t.Fatalf("RecordTaskUpdate() error = %v", err)
	}
	if store.event != "update" || store.upserted.Status != TaskDone || store.trigger != trigger {
		t.Fatalf("update event = %q, info = %+v, trigger = %+v", store.event, store.upserted, store.trigger)
	}
}

func TestRecordTaskMutationFallsBackToWriter(t *testing.T) {
	store := &recordingStore{}
	if err := RecordTaskUpsert(store, TaskInfo{ID: "task_1"}, TaskTrigger{Source: "console"}); err != nil {
		t.Fatalf("RecordTaskUpsert() error = %v", err)
	}
	if store.upserted.ID != "task_1" {
		t.Fatalf("upserted task = %+v", store.upserted)
	}
	if err := RecordTaskUpdate(store, "task_1", TaskTrigger{}, func(task *TaskInfo) {
		task.Status = TaskFailed
	}); err != nil {
		t.Fatalf("RecordTaskUpdate() error = %v", err)
	}
	if store.updated != "task_1" || store.upserted.Status != TaskFailed {
		t.Fatalf("updated task = %+v", store.upserted)
	}
}
