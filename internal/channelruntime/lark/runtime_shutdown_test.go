package lark

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	larkbus "github.com/quailyquaily/mistermorph/internal/bus/adapters/lark"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

func TestLarkRunnerShutdownCancelsQueuedTask(t *testing.T) {
	store := daemonruntime.NewMemoryStore(10)
	const taskID = "lark_queued_shutdown"
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
	daemonServer := &larkDaemonShutdownTestServer{shutdownCalled: make(chan struct{})}
	bus := &larkShutdownTestBus{closed: make(chan struct{}), mustFollow: daemonServer.shutdownCalled}
	runner := runtimecore.NewConversationRunner[string, larkJob](
		workersCtx,
		sem,
		1,
		func(context.Context, string, larkJob) { handled <- struct{}{} },
		larkConversationRunnerOptions(logger, store, nil),
	)
	if err := runner.Enqueue(context.Background(), "lark:chat", func(version uint64) larkJob {
		return larkJob{TaskID: taskID, ConversationKey: "lark:chat", Version: version}
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	shutdownLarkRuntime(daemonServer, nil, bus, stopWorkers, runner)

	assertLarkRuntimeClosedTask(t, store, taskID)
	assertLarkShutdownSignal(t, bus.closed, "bus close")
	if !bus.closedAfterDependency {
		t.Fatal("bus closed before daemon server shutdown")
	}
	select {
	case <-handled:
		t.Fatal("queued task was handled after shutdown")
	default:
	}
}

func TestLarkOwnedContextIsStoppedOnlyByRuntimeShutdown(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(context.Background())
	workersCtx, stopWorkers := newLarkOwnedContext(parentCtx)
	cancelParent()
	select {
	case <-workersCtx.Done():
		t.Fatal("worker context followed ingress cancellation before runtime shutdown")
	default:
	}

	stopWorkers()
	waitLarkShutdownSignal(t, workersCtx.Done(), "explicit worker shutdown")
}

func TestLarkRunnerShutdownWaitsForActiveTaskCancellation(t *testing.T) {
	store := daemonruntime.NewMemoryStore(10)
	const taskID = "lark_active_shutdown"
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
	daemonServer := &larkDaemonShutdownTestServer{shutdownCalled: make(chan struct{})}
	bus := &larkShutdownTestBus{closed: make(chan struct{}), mustFollow: daemonServer.shutdownCalled}
	runner := runtimecore.NewConversationRunner[string, larkJob](
		workersCtx,
		make(chan struct{}, 1),
		1,
		func(workerCtx context.Context, _ string, job larkJob) {
			if err := runtimecore.MarkTaskRunning(store, job.TaskID); err != nil {
				return
			}
			close(started)
			<-workerCtx.Done()
			canceled <- cancelLarkTaskOnWorkerShutdown(workerCtx, logger, store, job)
			close(finished)
		},
		larkConversationRunnerOptions(logger, store, nil),
	)
	if err := runner.Enqueue(context.Background(), "lark:chat", func(version uint64) larkJob {
		return larkJob{TaskID: taskID, ConversationKey: "lark:chat", Version: version}
	}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	waitLarkShutdownSignal(t, started, "active task start")

	shutdownLarkRuntime(daemonServer, nil, bus, stopWorkers, runner)

	waitLarkShutdownSignal(t, finished, "active task finish")
	select {
	case got := <-canceled:
		if !got {
			t.Fatal("cancelLarkTaskOnWorkerShutdown() = false, want true")
		}
	default:
		t.Fatal("active task cancellation result is missing")
	}
	assertLarkRuntimeClosedTask(t, store, taskID)
	if !bus.closedAfterDependency {
		t.Fatal("bus closed before daemon server shutdown")
	}
}

func TestLarkContextShutdownCancelsWorkersBeforeJoiningWebSocket(t *testing.T) {
	store := daemonruntime.NewMemoryStore(10)
	for _, taskID := range []string{"lark_shutdown_active", "lark_shutdown_queued", "lark_shutdown_blocked"} {
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
	runner := runtimecore.NewConversationRunner[string, larkJob](
		workersCtx,
		sem,
		1,
		nil,
		larkConversationRunnerOptions(nil, store, nil),
	)
	defer func() {
		stopWorkers()
		runner.WaitClosed()
	}()
	if err := runner.Enqueue(context.Background(), "lark:shutdown", func(version uint64) larkJob {
		return larkJob{TaskID: "lark_shutdown_active", ConversationKey: "lark:shutdown", Version: version}
	}); err != nil {
		t.Fatalf("Enqueue(active) error = %v", err)
	}
	queuedErr := make(chan error, 1)
	go func() {
		queuedErr <- runner.Enqueue(context.Background(), "lark:shutdown", func(version uint64) larkJob {
			return larkJob{TaskID: "lark_shutdown_queued", ConversationKey: "lark:shutdown", Version: version}
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
		err := runner.Enqueue(context.Background(), "lark:shutdown", func(version uint64) larkJob {
			close(handlerBuiltJob)
			return larkJob{TaskID: "lark_shutdown_blocked", ConversationKey: "lark:shutdown", Version: version}
		})
		if err != nil {
			if stateErr := runtimecore.MarkTaskFailed(store, "lark_shutdown_blocked", err.Error(), taskdomain.EndedByCancellation(context.Background(), err)); stateErr != nil {
				handlerErr <- stateErr
				close(handlerDone)
				return
			}
		}
		handlerErr <- err
		close(handlerDone)
	}()
	waitLarkShutdownSignal(t, handlerBuiltJob, "blocked bus handler enqueue")
	select {
	case <-handlerDone:
		t.Fatal("bus handler enqueue returned while the conversation queue was full")
	default:
	}

	client := &larkWebSocketShutdownTestClient{
		started:     make(chan struct{}),
		closeCalled: make(chan struct{}),
		release:     handlerDone,
	}
	ingressCtx, cancelIngress := context.WithCancel(context.Background())
	ingressDone := make(chan struct{})
	ingressErr := make(chan error, 1)
	shutdownOrderErr := make(chan error, 1)
	go func() {
		ingressErr <- runLarkWebSocketIngress(ingressCtx, larkWebSocketIngressOptions{
			Inbound: &larkbus.InboundAdapter{},
			Client:  client,
			StopWorkers: func() {
				select {
				case <-client.closeCalled:
				default:
					shutdownOrderErr <- errors.New("workers stopped before websocket admission")
				}
				stopWorkers()
			},
		})
		close(ingressDone)
	}()
	waitLarkShutdownSignal(t, client.started, "websocket ingress start")
	cancelIngress()
	select {
	case <-ingressDone:
	case <-time.After(500 * time.Millisecond):
		stopWorkers()
		waitLarkShutdownSignal(t, ingressDone, "deadlocked websocket ingress join")
		t.Fatal("websocket ingress join waited for the active callback before canceling workers")
	}
	if err := <-ingressErr; err != nil {
		t.Fatalf("runLarkWebSocketIngress() error = %v", err)
	}
	select {
	case err := <-shutdownOrderErr:
		t.Fatal(err)
	default:
	}

	bus := &larkShutdownTestBus{closed: make(chan struct{}), mustFollow: ingressDone}
	shutdownLarkRuntime(nil, nil, bus, stopWorkers, runner)
	if !bus.closedAfterDependency {
		t.Fatal("bus closed before websocket ingress joined")
	}

	if err := <-handlerErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked bus handler error = %v, want context canceled", err)
	}
	assertLarkRuntimeClosedTask(t, store, "lark_shutdown_active")
	assertLarkRuntimeClosedTask(t, store, "lark_shutdown_queued")
	assertLarkCanceledTask(t, store, "lark_shutdown_blocked")
}

func TestCancelLarkRuntimeTaskPreservesPendingAndTerminalTask(t *testing.T) {
	for _, status := range []daemonruntime.TaskStatus{
		daemonruntime.TaskPending,
		daemonruntime.TaskDone,
		daemonruntime.TaskFailed,
		daemonruntime.TaskCanceled,
	} {
		t.Run(string(status), func(t *testing.T) {
			store := daemonruntime.NewMemoryStore(10)
			finishedAt := time.Now().UTC().Add(-time.Minute)
			const taskID = "lark_terminal_shutdown"
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

			cancelLarkRuntimeTask(nil, store, larkJob{TaskID: taskID})

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

func TestLarkRunnerPanicFailsOnlyQueuedAndRunningTasks(t *testing.T) {
	for _, tc := range []struct {
		status     daemonruntime.TaskStatus
		wantStatus daemonruntime.TaskStatus
		wantError  string
	}{
		{status: daemonruntime.TaskQueued, wantStatus: daemonruntime.TaskFailed, wantError: larkWorkerPanicTaskError},
		{status: daemonruntime.TaskRunning, wantStatus: daemonruntime.TaskFailed, wantError: larkWorkerPanicTaskError},
		{status: daemonruntime.TaskPending, wantStatus: daemonruntime.TaskPending, wantError: "original state"},
		{status: daemonruntime.TaskDone, wantStatus: daemonruntime.TaskDone, wantError: "original state"},
		{status: daemonruntime.TaskFailed, wantStatus: daemonruntime.TaskFailed, wantError: "original state"},
		{status: daemonruntime.TaskCanceled, wantStatus: daemonruntime.TaskCanceled, wantError: "original state"},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			store := daemonruntime.NewMemoryStore(10)
			const taskID = "lark_panic_transition"
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
				Runtime:         "lark",
				ConversationKey: "lark:chat",
				TaskID:          taskID,
			})
			if err != nil {
				t.Fatalf("StartLease() error = %v", err)
			}
			defer lease.Finish()

			opts := larkConversationRunnerOptions(nil, store, control)
			opts.OnPanic("lark:chat", larkJob{TaskID: taskID, ConversationKey: "lark:chat"})

			replacement, err := control.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
				Runtime:         "lark",
				ConversationKey: "lark:chat",
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

func TestRunLarkWebSocketIngressWaitsForStartAfterClose(t *testing.T) {
	client := &larkWebSocketShutdownTestClient{
		started:     make(chan struct{}),
		closeCalled: make(chan struct{}),
		release:     make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan error, 1)
	go func() {
		returned <- runLarkWebSocketIngress(ctx, larkWebSocketIngressOptions{
			Inbound: &larkbus.InboundAdapter{},
			Client:  client,
		})
	}()
	waitLarkShutdownSignal(t, client.started, "websocket start")
	cancel()
	waitLarkShutdownSignal(t, client.closeCalled, "websocket close")
	select {
	case err := <-returned:
		t.Fatalf("runLarkWebSocketIngress() returned before Start exited: %v", err)
	default:
	}

	close(client.release)
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("runLarkWebSocketIngress() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runLarkWebSocketIngress() did not return after Start exited")
	}
}

func assertLarkRuntimeClosedTask(t *testing.T, store daemonruntime.TaskReader, taskID string) {
	t.Helper()
	task, ok := store.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("Get(%q) = %#v, %v", taskID, task, ok)
	}
	if task.Status != daemonruntime.TaskCanceled || task.Error != larkRuntimeClosedTaskError || task.FinishedAt == nil {
		t.Fatalf("task after shutdown = %#v, want canceled with runtime closed error", task)
	}
}

func assertLarkCanceledTask(t *testing.T, store daemonruntime.TaskReader, taskID string) {
	t.Helper()
	task, ok := store.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("Get(%q) = %#v, %v", taskID, task, ok)
	}
	if task.Status != daemonruntime.TaskCanceled || task.FinishedAt == nil {
		t.Fatalf("task after canceled enqueue = %#v, want terminal canceled task", task)
	}
}

func waitLarkShutdownSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func assertLarkShutdownSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	default:
		t.Fatalf("%s was not observed", name)
	}
}

type larkShutdownTestBus struct {
	closed                chan struct{}
	mustFollow            <-chan struct{}
	closedAfterDependency bool
	once                  sync.Once
}

type larkEnqueueDrainTestBus struct {
	handlerDone <-chan struct{}
}

func (b *larkEnqueueDrainTestBus) Close() error {
	if b != nil && b.handlerDone != nil {
		<-b.handlerDone
	}
	return nil
}

func (b *larkShutdownTestBus) Close() error {
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

type larkDaemonShutdownTestServer struct {
	shutdownCalled chan struct{}
	once           sync.Once
}

func (s *larkDaemonShutdownTestServer) Shutdown(context.Context) error {
	s.once.Do(func() { close(s.shutdownCalled) })
	return nil
}

type larkWebSocketShutdownTestClient struct {
	started     chan struct{}
	closeCalled chan struct{}
	release     chan struct{}
	closeOnce   sync.Once
}

func (c *larkWebSocketShutdownTestClient) Start(context.Context) error {
	close(c.started)
	<-c.release
	return nil
}

func (c *larkWebSocketShutdownTestClient) Close() {
	c.closeOnce.Do(func() { close(c.closeCalled) })
}
