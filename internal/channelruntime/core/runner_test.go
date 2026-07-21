package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

type runnerTestJob struct {
	Version uint64
	Value   string
}

func TestConversationRunnerEnqueueUsesCurrentVersion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sem := make(chan struct{}, 1)
	handled := make(chan runnerTestJob, 2)
	r := NewConversationRunner[string, runnerTestJob](
		ctx,
		sem,
		4,
		func(_ context.Context, _ string, job runnerTestJob) {
			handled <- job
		},
		ConversationRunnerOptions[string, runnerTestJob]{},
	)

	if err := r.Enqueue(context.Background(), "conv:a", func(version uint64) runnerTestJob {
		return runnerTestJob{Version: version, Value: "first"}
	}); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	r.IncrementVersion("conv:a")
	if err := r.Enqueue(context.Background(), "conv:a", func(version uint64) runnerTestJob {
		return runnerTestJob{Version: version, Value: "second"}
	}); err != nil {
		t.Fatalf("enqueue second: %v", err)
	}

	first := readRunnerJob(t, handled)
	second := readRunnerJob(t, handled)
	if first.Value != "first" || first.Version != 0 {
		t.Fatalf("first job = %#v, want value=first version=0", first)
	}
	if second.Value != "second" || second.Version != 1 {
		t.Fatalf("second job = %#v, want value=second version=1", second)
	}
}

func TestConversationRunnerCurrentVersionDefault(t *testing.T) {
	r := NewConversationRunner[string, runnerTestJob](
		context.Background(),
		make(chan struct{}, 1),
		2,
		nil,
		ConversationRunnerOptions[string, runnerTestJob]{},
	)
	if got := r.CurrentVersion("missing"); got != 0 {
		t.Fatalf("current version = %d, want 0", got)
	}
}

func TestConversationRunnerEnqueueRequiresBuilder(t *testing.T) {
	r := NewConversationRunner[string, runnerTestJob](
		context.Background(),
		make(chan struct{}, 1),
		2,
		nil,
		ConversationRunnerOptions[string, runnerTestJob]{},
	)
	if err := r.Enqueue(context.Background(), "k", nil); err == nil {
		t.Fatalf("enqueue(nil builder) should fail")
	}
}

func TestConversationRunnerRejectsEnqueueAfterWorkerContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := NewConversationRunner[string, runnerTestJob](
		ctx,
		make(chan struct{}, 1),
		1,
		nil,
		ConversationRunnerOptions[string, runnerTestJob]{},
	)
	cancel()
	for i := 0; i < 100; i++ {
		err := r.Enqueue(context.Background(), "closed", func(uint64) runnerTestJob {
			return runnerTestJob{Value: "must not enqueue"}
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Enqueue() attempt %d error = %v, want context canceled", i, err)
		}
	}
}

func TestConversationRunnerDropsAcceptedJobsWhenWorkerContextCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	dropped := make(chan runnerTestJob, 2)
	handled := make(chan runnerTestJob, 2)
	r := NewConversationRunner[string, runnerTestJob](
		ctx,
		sem,
		2,
		func(_ context.Context, _ string, job runnerTestJob) {
			handled <- job
		},
		ConversationRunnerOptions[string, runnerTestJob]{
			OnDrop: func(_ string, job runnerTestJob) {
				dropped <- job
			},
		},
	)
	for _, value := range []string{"first", "second"} {
		value := value
		if err := r.Enqueue(context.Background(), "queued", func(uint64) runnerTestJob {
			return runnerTestJob{Value: value}
		}); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", value, err)
		}
	}

	cancel()
	r.WaitClosed()
	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case job := <-dropped:
			got[job.Value] = true
		case job := <-handled:
			t.Fatalf("handled job after worker context close: %#v", job)
		case <-time.After(time.Second):
			t.Fatalf("dropped jobs = %#v, want first and second", got)
		}
	}
}

func TestConversationRunnerReclaimsIdleWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handled := make(chan runnerTestJob, 1)
	r := NewConversationRunner[string, runnerTestJob](
		ctx,
		make(chan struct{}, 1),
		2,
		func(_ context.Context, _ string, job runnerTestJob) {
			handled <- job
		},
		ConversationRunnerOptions[string, runnerTestJob]{IdleTimeout: 10 * time.Millisecond},
	)
	if err := r.Enqueue(context.Background(), "idle", func(version uint64) runnerTestJob {
		return runnerTestJob{Version: version, Value: "handled"}
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if got := readRunnerJob(t, handled); got.Value != "handled" {
		t.Fatalf("handled job = %#v", got)
	}
	waitRunnerWorkerCount(t, r, 0, 500*time.Millisecond)
	if err := r.Enqueue(context.Background(), "idle", func(version uint64) runnerTestJob {
		return runnerTestJob{Version: version, Value: "after-reclaim"}
	}); err != nil {
		t.Fatalf("Enqueue(after reclaim) error = %v", err)
	}
	if got := readRunnerJob(t, handled); got.Value != "after-reclaim" {
		t.Fatalf("handled job after reclaim = %#v", got)
	}
}

func TestConversationRunnerIdleRetirementDoesNotLoseReservedEnqueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	idleTimeout := 15 * time.Millisecond
	handled := make(chan runnerTestJob, 2)
	r := NewConversationRunner[string, runnerTestJob](
		ctx,
		make(chan struct{}, 1),
		1,
		func(_ context.Context, _ string, job runnerTestJob) {
			handled <- job
		},
		ConversationRunnerOptions[string, runnerTestJob]{IdleTimeout: idleTimeout},
	)
	if err := r.Enqueue(context.Background(), "race", func(version uint64) runnerTestJob {
		return runnerTestJob{Version: version, Value: "first"}
	}); err != nil {
		t.Fatalf("Enqueue(first) error = %v", err)
	}
	if got := readRunnerJob(t, handled); got.Value != "first" {
		t.Fatalf("first handled job = %#v", got)
	}

	builderEntered := make(chan struct{})
	releaseBuilder := make(chan struct{})
	enqueueErr := make(chan error, 1)
	go func() {
		enqueueErr <- r.Enqueue(context.Background(), "race", func(version uint64) runnerTestJob {
			close(builderEntered)
			<-releaseBuilder
			return runnerTestJob{Version: version, Value: "second"}
		})
	}()
	select {
	case <-builderEntered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reserved enqueue did not enter builder")
	}
	select {
	case <-time.After(4 * idleTimeout):
	case err := <-enqueueErr:
		t.Fatalf("reserved enqueue returned before builder release: %v", err)
	}
	if got := runnerWorkerCount(r); got != 1 {
		t.Fatalf("worker count while enqueue is reserved = %d, want 1", got)
	}

	close(releaseBuilder)
	select {
	case err := <-enqueueErr:
		if err != nil {
			t.Fatalf("Enqueue(second) error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reserved enqueue remained blocked after builder release")
	}
	if got := readRunnerJob(t, handled); got.Value != "second" {
		t.Fatalf("second handled job = %#v", got)
	}
	waitRunnerWorkerCount(t, r, 0, 500*time.Millisecond)
}

type runnerPanicJob struct {
	TaskID string
	Panic  bool
}

func TestConversationRunnerRecoversPanicMarksTaskFailedAndContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	root := t.TempDir()
	store, err := daemonruntime.NewFileTaskStore(daemonruntime.FileTaskStoreOptions{
		RootDir:    filepath.Join(root, "tasks"),
		Target:     "runner-test",
		Persist:    true,
		JournalDir: filepath.Join(root, "journal"),
	})
	if err != nil {
		t.Fatalf("NewFileTaskStore() error = %v", err)
	}
	if err := store.Upsert(daemonruntime.TaskInfo{
		ID:        "panic-task",
		Status:    daemonruntime.TaskRunning,
		Task:      "panic",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("store.Upsert() error = %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	panicHandled := make(chan error, 1)
	nextHandled := make(chan struct{}, 1)
	r := NewConversationRunner[string, runnerPanicJob](
		ctx,
		make(chan struct{}, 1),
		2,
		func(_ context.Context, _ string, job runnerPanicJob) {
			if job.Panic {
				panic("runner boom")
			}
			nextHandled <- struct{}{}
		},
		ConversationRunnerOptions[string, runnerPanicJob]{
			IdleTimeout: time.Second,
			Logger:      logger,
			OnPanic: func(_ string, job runnerPanicJob) {
				panicHandled <- MarkTaskFailed(store, job.TaskID, "conversation worker panicked", false)
			},
		},
	)
	if err := r.Enqueue(context.Background(), "same", func(uint64) runnerPanicJob {
		return runnerPanicJob{TaskID: "panic-task", Panic: true}
	}); err != nil {
		t.Fatalf("Enqueue(panic) error = %v", err)
	}
	if err := r.Enqueue(context.Background(), "same", func(uint64) runnerPanicJob {
		return runnerPanicJob{TaskID: "next-task"}
	}); err != nil {
		t.Fatalf("Enqueue(next) error = %v", err)
	}
	select {
	case err := <-panicHandled:
		if err != nil {
			t.Fatalf("panic task state write error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("panic callback was not called")
	}
	select {
	case <-nextHandled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("worker did not process the job after a panic")
	}

	task, ok := store.Get("panic-task")
	if !ok || task == nil || task.Status != daemonruntime.TaskFailed || task.Error != "conversation worker panicked" {
		t.Fatalf("panic task = %#v, ok=%v; want durable failed state", task, ok)
	}
	reloaded, err := daemonruntime.NewFileTaskStore(daemonruntime.FileTaskStoreOptions{
		RootDir:    filepath.Join(root, "tasks"),
		Target:     "runner-test",
		Persist:    true,
		JournalDir: filepath.Join(root, "journal"),
	})
	if err != nil {
		t.Fatalf("reload NewFileTaskStore() error = %v", err)
	}
	persisted, ok := reloaded.Get("panic-task")
	if !ok || persisted == nil || persisted.Status != daemonruntime.TaskFailed {
		t.Fatalf("reloaded panic task = %#v, ok=%v; want failed", persisted, ok)
	}
	logText := logs.String()
	if !strings.Contains(logText, "conversation_worker_job_panic") ||
		!strings.Contains(logText, "runner boom") ||
		!strings.Contains(logText, `"stack"`) {
		t.Fatalf("panic log = %q, want event, panic value, and stack", logText)
	}
}

func TestConversationRunnerRecoversPanicFromOnPanicCallback(t *testing.T) {
	var logs bytes.Buffer
	r := NewConversationRunner[string, runnerPanicJob](
		context.Background(),
		make(chan struct{}, 1),
		1,
		func(context.Context, string, runnerPanicJob) {
			panic("job panic")
		},
		ConversationRunnerOptions[string, runnerPanicJob]{
			Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
			OnPanic: func(string, runnerPanicJob) {
				panic("panic callback panic")
			},
		},
	)

	var escaped any
	func() {
		defer func() { escaped = recover() }()
		r.handleJob("conversation", runnerPanicJob{TaskID: "panic-task", Panic: true})
	}()
	if escaped != nil {
		t.Fatalf("OnPanic callback panic escaped: %v", escaped)
	}
	logText := logs.String()
	if !strings.Contains(logText, "conversation_worker_job_panic") ||
		!strings.Contains(logText, "conversation_worker_panic_callback_panic") ||
		!strings.Contains(logText, "panic callback panic") {
		t.Fatalf("panic log = %q, want job and callback panic events", logText)
	}
}

func waitRunnerWorkerCount[K comparable, J any](t *testing.T, r *ConversationRunner[K, J], want int, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if runnerWorkerCount(r) == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("worker count = %d, want %d", runnerWorkerCount(r), want)
		case <-ticker.C:
		}
	}
}

func runnerWorkerCount[K comparable, J any](r *ConversationRunner[K, J]) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.workers)
}

func readRunnerJob(t *testing.T, ch <-chan runnerTestJob) runnerTestJob {
	t.Helper()
	select {
	case job := <-ch:
		return job
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for runner job")
		return runnerTestJob{}
	}
}
