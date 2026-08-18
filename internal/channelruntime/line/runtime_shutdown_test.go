package line

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

func TestLineRunnerShutdownCancelsQueuedTask(t *testing.T) {
	store := daemonruntime.NewMemoryStore(10)
	const taskID = "line_queued_shutdown"
	if err := store.Upsert(daemonruntime.TaskInfo{
		ID:        taskID,
		Status:    daemonruntime.TaskQueued,
		Task:      "queued",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	workersCtx, stopWorkers := context.WithCancel(context.Background())
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	handled := make(chan struct{}, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	daemonServer := &lineWebhookShutdownTestServer{shutdownCalled: make(chan struct{})}
	bus := &lineShutdownTestBus{closed: make(chan struct{}), mustFollow: daemonServer.shutdownCalled}
	runner := runtimecore.NewConversationRunner[string, lineJob](
		workersCtx,
		sem,
		1,
		func(context.Context, string, lineJob) { handled <- struct{}{} },
		lineConversationRunnerOptions(logger, store, nil),
	)
	if err := runner.Enqueue(context.Background(), "line:chat", func(version uint64) lineJob {
		return lineJob{TaskID: taskID, ConversationKey: "line:chat", Version: version}
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	shutdownLineRuntime(daemonServer, nil, bus, stopWorkers, runner)

	assertLineRuntimeClosedTask(t, store, taskID)
	assertLineShutdownSignal(t, bus.closed, "bus close")
	if !bus.closedAfterDependency {
		t.Fatal("bus closed before daemon server shutdown")
	}
	select {
	case <-handled:
		t.Fatal("queued task was handled after shutdown")
	default:
	}
}

func TestLineOwnedContextIsStoppedOnlyByRuntimeShutdown(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(context.Background())
	workersCtx, stopWorkers := newLineOwnedContext(parentCtx)
	cancelParent()
	select {
	case <-workersCtx.Done():
		t.Fatal("worker context followed ingress cancellation before runtime shutdown")
	default:
	}

	stopWorkers()
	waitLineShutdownSignal(t, workersCtx.Done(), "explicit worker shutdown")
}

func TestLineRunnerShutdownWaitsForActiveTaskCancellation(t *testing.T) {
	store := daemonruntime.NewMemoryStore(10)
	const taskID = "line_active_shutdown"
	if err := store.Upsert(daemonruntime.TaskInfo{
		ID:        taskID,
		Status:    daemonruntime.TaskQueued,
		Task:      "active",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	workersCtx, stopWorkers := context.WithCancel(context.Background())
	started := make(chan struct{})
	finished := make(chan struct{})
	canceled := make(chan bool, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	daemonServer := &lineWebhookShutdownTestServer{shutdownCalled: make(chan struct{})}
	bus := &lineShutdownTestBus{closed: make(chan struct{}), mustFollow: daemonServer.shutdownCalled}
	runner := runtimecore.NewConversationRunner[string, lineJob](
		workersCtx,
		make(chan struct{}, 1),
		1,
		func(workerCtx context.Context, _ string, job lineJob) {
			if err := runtimecore.MarkTaskRunning(store, job.TaskID); err != nil {
				return
			}
			close(started)
			<-workerCtx.Done()
			canceled <- cancelLineTaskOnWorkerShutdown(workerCtx, logger, store, job)
			close(finished)
		},
		lineConversationRunnerOptions(logger, store, nil),
	)
	if err := runner.Enqueue(context.Background(), "line:chat", func(version uint64) lineJob {
		return lineJob{TaskID: taskID, ConversationKey: "line:chat", Version: version}
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	waitLineShutdownSignal(t, started, "active task start")

	shutdownLineRuntime(daemonServer, nil, bus, stopWorkers, runner)

	waitLineShutdownSignal(t, finished, "active task finish")
	select {
	case got := <-canceled:
		if !got {
			t.Fatal("cancelLineTaskOnWorkerShutdown() = false, want true")
		}
	default:
		t.Fatal("active task cancellation result is missing")
	}
	assertLineRuntimeClosedTask(t, store, taskID)
	if !bus.closedAfterDependency {
		t.Fatal("bus closed before daemon server shutdown")
	}
}

func TestLineContextShutdownCancelsWorkersBeforeJoiningWebhook(t *testing.T) {
	store := daemonruntime.NewMemoryStore(10)
	for _, taskID := range []string{"line_shutdown_active", "line_shutdown_queued", "line_shutdown_blocked"} {
		if err := store.Upsert(daemonruntime.TaskInfo{
			ID:        taskID,
			Status:    daemonruntime.TaskQueued,
			Task:      taskID,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("Upsert(%q) error = %v", taskID, err)
		}
	}

	workersCtx, stopWorkers := context.WithCancel(context.Background())
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	runner := runtimecore.NewConversationRunner[string, lineJob](
		workersCtx,
		sem,
		1,
		nil,
		lineConversationRunnerOptions(nil, store, nil),
	)
	defer func() {
		stopWorkers()
		runner.WaitClosed()
	}()
	if err := runner.Enqueue(context.Background(), "line:shutdown", func(version uint64) lineJob {
		return lineJob{TaskID: "line_shutdown_active", ConversationKey: "line:shutdown", Version: version}
	}); err != nil {
		t.Fatalf("Enqueue(active) error = %v", err)
	}
	queuedErr := make(chan error, 1)
	go func() {
		queuedErr <- runner.Enqueue(context.Background(), "line:shutdown", func(version uint64) lineJob {
			return lineJob{TaskID: "line_shutdown_queued", ConversationKey: "line:shutdown", Version: version}
		})
	}()
	select {
	case err := <-queuedErr:
		if err != nil {
			t.Fatalf("Enqueue(queued) error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out filling the conversation queue")
	}

	handlerBuiltJob := make(chan struct{})
	handlerDone := make(chan struct{})
	handlerErr := make(chan error, 1)
	go func() {
		err := runner.Enqueue(context.Background(), "line:shutdown", func(version uint64) lineJob {
			close(handlerBuiltJob)
			return lineJob{TaskID: "line_shutdown_blocked", ConversationKey: "line:shutdown", Version: version}
		})
		if err != nil {
			if stateErr := runtimecore.MarkTaskFailed(store, "line_shutdown_blocked", err.Error(), taskdomain.EndedByCancellation(context.Background(), err)); stateErr != nil {
				handlerErr <- stateErr
				close(handlerDone)
				return
			}
		}
		handlerErr <- err
		close(handlerDone)
	}()
	waitLineShutdownSignal(t, handlerBuiltJob, "blocked bus handler enqueue")
	select {
	case <-handlerDone:
		t.Fatal("bus handler enqueue returned while the conversation queue was full")
	default:
	}

	serveDone := make(chan error, 1)
	webhookServer := &lineWebhookIngressJoinTestServer{
		admissionStopped: make(chan struct{}),
		handlerDone:      handlerDone,
		joined:           make(chan struct{}),
		serveDone:        serveDone,
	}
	bus := &lineShutdownTestBus{closed: make(chan struct{}), mustFollow: webhookServer.joined}
	shutdownDone := make(chan struct{})
	shutdownErr := make(chan error, 1)
	go func() {
		shutdownErr <- stopAndWaitLineWebhook(webhookServer, serveDone, stopWorkers)
		shutdownLineRuntime(nil, nil, bus, stopWorkers, runner)
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
	case <-time.After(500 * time.Millisecond):
		stopWorkers()
		waitLineShutdownSignal(t, shutdownDone, "deadlocked runtime shutdown cleanup")
		t.Fatal("webhook shutdown waited for the active handler before canceling workers")
	}
	if err := <-shutdownErr; err != nil {
		t.Fatalf("stopAndWaitLineWebhook() error = %v", err)
	}
	if !bus.closedAfterDependency {
		t.Fatal("bus closed before webhook ingress joined")
	}

	if err := <-handlerErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked bus handler error = %v, want context canceled", err)
	}
	assertLineRuntimeClosedTask(t, store, "line_shutdown_active")
	assertLineRuntimeClosedTask(t, store, "line_shutdown_queued")
	assertLineCanceledTask(t, store, "line_shutdown_blocked")
}

func TestCancelLineRuntimeTaskPreservesPendingAndTerminalTask(t *testing.T) {
	for _, status := range []daemonruntime.TaskStatus{
		daemonruntime.TaskPending,
		daemonruntime.TaskDone,
		daemonruntime.TaskFailed,
		daemonruntime.TaskCanceled,
	} {
		t.Run(string(status), func(t *testing.T) {
			store := daemonruntime.NewMemoryStore(10)
			finishedAt := time.Now().UTC().Add(-time.Minute)
			const taskID = "line_terminal_shutdown"
			if err := store.Upsert(daemonruntime.TaskInfo{
				ID:         taskID,
				Status:     status,
				Task:       "terminal",
				Error:      "original terminal state",
				CreatedAt:  finishedAt.Add(-time.Minute),
				FinishedAt: &finishedAt,
			}); err != nil {
				t.Fatalf("Upsert() error = %v", err)
			}

			cancelLineRuntimeTask(nil, store, lineJob{TaskID: taskID})

			task, ok := store.Get(taskID)
			if !ok || task == nil {
				t.Fatalf("Get(%q) = %#v, %v", taskID, task, ok)
			}
			if task.Status != status || task.Error != "original terminal state" || task.FinishedAt == nil || !task.FinishedAt.Equal(finishedAt) {
				t.Fatalf("terminal task after shutdown = %#v, want original terminal state", task)
			}
		})
	}
}

func TestLineRunnerPanicFailsOnlyQueuedAndRunningTasks(t *testing.T) {
	for _, tc := range []struct {
		status     daemonruntime.TaskStatus
		wantStatus daemonruntime.TaskStatus
		wantError  string
	}{
		{status: daemonruntime.TaskQueued, wantStatus: daemonruntime.TaskFailed, wantError: lineWorkerPanicTaskError},
		{status: daemonruntime.TaskRunning, wantStatus: daemonruntime.TaskFailed, wantError: lineWorkerPanicTaskError},
		{status: daemonruntime.TaskPending, wantStatus: daemonruntime.TaskPending, wantError: "original state"},
		{status: daemonruntime.TaskDone, wantStatus: daemonruntime.TaskDone, wantError: "original state"},
		{status: daemonruntime.TaskFailed, wantStatus: daemonruntime.TaskFailed, wantError: "original state"},
		{status: daemonruntime.TaskCanceled, wantStatus: daemonruntime.TaskCanceled, wantError: "original state"},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			store := daemonruntime.NewMemoryStore(10)
			const taskID = "line_panic_transition"
			finishedAt := time.Now().UTC().Add(-time.Minute)
			if err := store.Upsert(daemonruntime.TaskInfo{
				ID:         taskID,
				Status:     tc.status,
				Task:       "panic",
				Error:      "original state",
				CreatedAt:  finishedAt.Add(-time.Minute),
				FinishedAt: &finishedAt,
			}); err != nil {
				t.Fatalf("Upsert() error = %v", err)
			}

			control := runtimecontrol.New()
			lease, err := control.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
				Runtime:         "line",
				ConversationKey: "line:chat",
				TaskID:          taskID,
			})
			if err != nil {
				t.Fatalf("StartLease() error = %v", err)
			}
			defer lease.Finish()

			opts := lineConversationRunnerOptions(nil, store, control)
			opts.OnPanic("line:chat", lineJob{TaskID: taskID, ConversationKey: "line:chat"})

			replacement, err := control.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
				Runtime:         "line",
				ConversationKey: "line:chat",
				TaskID:          taskID + "_replacement",
			})
			if err != nil {
				t.Fatalf("StartLease(replacement) error = %v", err)
			}
			replacement.Finish()

			task, ok := store.Get(taskID)
			if !ok || task == nil {
				t.Fatalf("Get(%q) = %#v, %v", taskID, task, ok)
			}
			if task.Status != tc.wantStatus || task.Error != tc.wantError || task.FinishedAt == nil {
				t.Fatalf("task after panic = %#v, want status %q error %q", task, tc.wantStatus, tc.wantError)
			}
			if tc.status != daemonruntime.TaskQueued && tc.status != daemonruntime.TaskRunning && !task.FinishedAt.Equal(finishedAt) {
				t.Fatalf("preserved task finished_at = %v, want %v", task.FinishedAt, finishedAt)
			}
		})
	}
}

func TestStopAndWaitLineWebhookWaitsForServeLoop(t *testing.T) {
	server := &lineWebhookShutdownTestServer{shutdownCalled: make(chan struct{})}
	serveDone := make(chan error, 1)
	returned := make(chan error, 1)
	go func() {
		returned <- stopAndWaitLineWebhook(server, serveDone, nil)
	}()
	waitLineShutdownSignal(t, server.shutdownCalled, "webhook shutdown")
	select {
	case err := <-returned:
		t.Fatalf("stopAndWaitLineWebhook() returned before serve loop exited: %v", err)
	default:
	}

	serveDone <- nil
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("stopAndWaitLineWebhook() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stopAndWaitLineWebhook() did not return after serve loop exited")
	}
}

func assertLineRuntimeClosedTask(t *testing.T, store daemonruntime.TaskReader, taskID string) {
	t.Helper()
	task, ok := store.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("Get(%q) = %#v, %v", taskID, task, ok)
	}
	if task.Status != daemonruntime.TaskCanceled || task.Error != lineRuntimeClosedTaskError || task.FinishedAt == nil {
		t.Fatalf("task after shutdown = %#v, want canceled with runtime closed error", task)
	}
}

func assertLineCanceledTask(t *testing.T, store daemonruntime.TaskReader, taskID string) {
	t.Helper()
	task, ok := store.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("Get(%q) = %#v, %v", taskID, task, ok)
	}
	if task.Status != daemonruntime.TaskCanceled || task.FinishedAt == nil {
		t.Fatalf("task after canceled enqueue = %#v, want terminal canceled task", task)
	}
}

func waitLineShutdownSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func assertLineShutdownSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	default:
		t.Fatalf("%s was not observed", name)
	}
}

type lineShutdownTestBus struct {
	closed                chan struct{}
	mustFollow            <-chan struct{}
	closedAfterDependency bool
	once                  sync.Once
}

func (b *lineShutdownTestBus) Close() error {
	b.once.Do(func() {
		if b.mustFollow == nil {
			b.closedAfterDependency = true
		} else {
			select {
			case <-b.mustFollow:
				b.closedAfterDependency = true
			default:
			}
		}
		close(b.closed)
	})
	return nil
}

type lineWebhookShutdownTestServer struct {
	shutdownCalled chan struct{}
	once           sync.Once
}

func (s *lineWebhookShutdownTestServer) Shutdown(context.Context) error {
	s.once.Do(func() { close(s.shutdownCalled) })
	return nil
}

func (s *lineWebhookShutdownTestServer) RegisterOnShutdown(func()) {}

type lineWebhookIngressJoinTestServer struct {
	admissionStopped chan struct{}
	handlerDone      <-chan struct{}
	joined           chan struct{}
	serveDone        chan<- error
	callbacksMu      sync.Mutex
	callbacks        []func()
	once             sync.Once
}

func (s *lineWebhookIngressJoinTestServer) RegisterOnShutdown(callback func()) {
	if callback == nil {
		return
	}
	s.callbacksMu.Lock()
	s.callbacks = append(s.callbacks, callback)
	s.callbacksMu.Unlock()
}

func (s *lineWebhookIngressJoinTestServer) Shutdown(context.Context) error {
	s.once.Do(func() {
		close(s.admissionStopped)
		s.callbacksMu.Lock()
		callbacks := append([]func(){}, s.callbacks...)
		s.callbacksMu.Unlock()
		for _, callback := range callbacks {
			callback()
		}
		<-s.handlerDone
		close(s.joined)
		s.serveDone <- nil
	})
	return nil
}
