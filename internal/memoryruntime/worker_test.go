package memoryruntime

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/memory"
)

func TestProjectionWorkerCountTrigger(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{CheckpointBatch: 10})

	worker, err := NewProjectionWorker(j, p, ProjectionWorkerOptions{
		Interval:           time.Hour,
		NewRecordThreshold: 2,
		ProjectLimit:       10,
		MaxRounds:          5,
	})
	if err != nil {
		t.Fatalf("NewProjectionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := j.Append(baseWorkerEvent("evt_1", "run_1", "2026-03-05T08:01:00Z", "tg:1")); err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}
	worker.NotifyRecordAppended()

	time.Sleep(80 * time.Millisecond)
	if _, ok, err := j.LoadCheckpoint(); err != nil {
		t.Fatalf("LoadCheckpoint() error = %v", err)
	} else if ok {
		t.Fatalf("checkpoint exists before threshold, want no checkpoint")
	}

	if err := j.Append(baseWorkerEvent("evt_2", "run_2", "2026-03-05T08:02:00Z", "tg:1")); err != nil {
		t.Fatalf("Append(evt_2) error = %v", err)
	}
	worker.NotifyRecordAppended()

	waitForCheckpointLine(t, j, 2, 2*time.Second)
}

func TestProjectionWorkerTimerTrigger(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{CheckpointBatch: 10})

	worker, err := NewProjectionWorker(j, p, ProjectionWorkerOptions{
		Interval:           20 * time.Millisecond,
		NewRecordThreshold: 100,
		ProjectLimit:       10,
		MaxRounds:          5,
	})
	if err != nil {
		t.Fatalf("NewProjectionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := j.Append(baseWorkerEvent("evt_1", "run_1", "2026-03-05T09:01:00Z", "tg:2")); err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}

	waitForCheckpointLine(t, j, 1, 2*time.Second)
}

func TestProjectionWorkerBoundedRounds(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{CheckpointBatch: 10})

	for i := 1; i <= 3; i++ {
		if err := j.Append(baseWorkerEvent(
			"evt_"+strconv.Itoa(i),
			"run_"+strconv.Itoa(i),
			"2026-03-05T10:0"+strconv.Itoa(i)+":00Z",
			"tg:3",
		)); err != nil {
			t.Fatalf("Append(event %d) error = %v", i, err)
		}
	}

	worker, err := NewProjectionWorker(j, p, ProjectionWorkerOptions{
		Interval:           time.Hour,
		NewRecordThreshold: 1,
		ProjectLimit:       1,
		MaxRounds:          1,
	})
	if err != nil {
		t.Fatalf("NewProjectionWorker() error = %v", err)
	}

	if err := worker.runProjection(context.Background(), projectionTriggerTimer); err != nil {
		t.Fatalf("runProjection() error = %v", err)
	}

	cp, ok, err := j.LoadCheckpoint()
	if err != nil {
		t.Fatalf("LoadCheckpoint() error = %v", err)
	}
	if !ok {
		t.Fatalf("LoadCheckpoint() ok = false, want true")
	}
	if cp.Line != 1 {
		t.Fatalf("checkpoint line = %d, want 1", cp.Line)
	}
}

func TestProjectionWorkerHasAtLeastUnprojectedHonorsCheckpoint(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{CheckpointBatch: 10})

	for i := 1; i <= 3; i++ {
		err := j.Append(baseWorkerEvent(
			"evt_"+strconv.Itoa(i),
			"run_"+strconv.Itoa(i),
			"2026-03-05T11:0"+strconv.Itoa(i)+":00Z",
			"tg:4",
		))
		if err != nil {
			t.Fatalf("Append(event %d) error = %v", i, err)
		}
	}

	worker, err := NewProjectionWorker(j, p, ProjectionWorkerOptions{
		Interval:           time.Hour,
		NewRecordThreshold: 2,
		ProjectLimit:       10,
		MaxRounds:          5,
	})
	if err != nil {
		t.Fatalf("NewProjectionWorker() error = %v", err)
	}

	ok, err := worker.hasAtLeastUnprojected(2)
	if err != nil {
		t.Fatalf("hasAtLeastUnprojected(2) error = %v", err)
	}
	if !ok {
		t.Fatalf("hasAtLeastUnprojected(2) = false, want true")
	}

	if err := j.SaveCheckpoint(memory.JournalCheckpoint{
		File: "events.000000000000000001.jsonl",
		Line: 2,
	}); err != nil {
		t.Fatalf("SaveCheckpoint() error = %v", err)
	}

	ok, err = worker.hasAtLeastUnprojected(2)
	if err != nil {
		t.Fatalf("hasAtLeastUnprojected(2 after cp) error = %v", err)
	}
	if ok {
		t.Fatalf("hasAtLeastUnprojected(2 after cp) = true, want false")
	}

	ok, err = worker.hasAtLeastUnprojected(1)
	if err != nil {
		t.Fatalf("hasAtLeastUnprojected(1 after cp) error = %v", err)
	}
	if !ok {
		t.Fatalf("hasAtLeastUnprojected(1 after cp) = false, want true")
	}
}

func TestProjectionWorkerRunProjectionSkipsWhenNoNewRecords(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{CheckpointBatch: 10})

	worker, err := NewProjectionWorker(j, p, ProjectionWorkerOptions{
		Interval:           time.Hour,
		NewRecordThreshold: 1,
		ProjectLimit:       1,
		MaxRounds:          1,
	})
	if err != nil {
		t.Fatalf("NewProjectionWorker() error = %v", err)
	}

	if err := worker.runProjection(context.Background(), projectionTriggerTimer); err != nil {
		t.Fatalf("runProjection() error = %v", err)
	}

	if _, ok, err := j.LoadCheckpoint(); err != nil {
		t.Fatalf("LoadCheckpoint() error = %v", err)
	} else if ok {
		t.Fatalf("checkpoint exists with empty journal, want no checkpoint")
	}
}

func TestProjectionWorkerTriggerSkipsWhenAlreadyRunning(t *testing.T) {
	root := t.TempDir()
	mgr := memory.NewManager(root, 7)
	j := newTestDomainJournal(t, root)
	p := memory.NewProjector(mgr, j, memory.ProjectorOptions{CheckpointBatch: 10})

	worker, err := NewProjectionWorker(j, p, ProjectionWorkerOptions{
		Interval:           time.Hour,
		NewRecordThreshold: 1,
		ProjectLimit:       10,
		MaxRounds:          2,
	})
	if err != nil {
		t.Fatalf("NewProjectionWorker() error = %v", err)
	}

	if err := j.Append(baseWorkerEvent("evt_1", "run_1", "2026-03-05T12:01:00Z", "tg:5")); err != nil {
		t.Fatalf("Append(evt_1) error = %v", err)
	}

	worker.running.Store(true)
	worker.trigger(context.Background(), projectionTriggerTimer)
	time.Sleep(80 * time.Millisecond)
	if _, ok, err := j.LoadCheckpoint(); err != nil {
		t.Fatalf("LoadCheckpoint() error = %v", err)
	} else if ok {
		t.Fatalf("checkpoint exists while running=true, want no checkpoint")
	}

	worker.running.Store(false)
	worker.trigger(context.Background(), projectionTriggerTimer)
	waitForCheckpointLine(t, j, 1, 2*time.Second)
}

func waitForCheckpointLine(t *testing.T, j *memory.DomainJournal, want int, timeout time.Duration) {
	t.Helper()
	wantLine := int64(want)
	deadline := time.Now().Add(timeout)
	for {
		cp, ok, err := j.LoadCheckpoint()
		if err != nil {
			t.Fatalf("LoadCheckpoint() error = %v", err)
		}
		if ok && cp.Line >= wantLine {
			return
		}
		if time.Now().After(deadline) {
			if ok {
				t.Fatalf("checkpoint line = %d, want >= %d", cp.Line, wantLine)
			}
			t.Fatalf("checkpoint missing, want line >= %d", wantLine)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func baseWorkerEvent(eventID, runID, tsUTC, subjectID string) memory.MemoryEvent {
	return memory.MemoryEvent{
		SchemaVersion: memory.CurrentMemoryEventSchemaVersion,
		EventID:       eventID,
		TaskRunID:     runID,
		TSUTC:         tsUTC,
		SessionID:     subjectID,
		SubjectID:     subjectID,
		Channel:       "telegram",
		TaskText:      "task",
		FinalOutput:   "output",
	}
}
