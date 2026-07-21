package consolecmd

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/spf13/viper"
)

func TestConsoleExecutionStateCloseReleasesPendingOwnership(t *testing.T) {
	state := newConsoleExecutionState(nil, nil)
	jobGeneration := &consoleLocalRuntimeGeneration{}
	approvalGeneration := &consoleLocalRuntimeGeneration{}
	jobGeneration.acquire()
	approvalGeneration.acquire()

	if err := state.addPendingJob(consoleLocalTaskJob{TaskID: "task_pending", Generation: jobGeneration}); err != nil {
		t.Fatalf("addPendingJob() error = %v", err)
	}
	if err := state.addPendingApproval("approval_pending", consoleLocalTaskJob{TaskID: "task_approval", Generation: approvalGeneration}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("addPendingApproval() error = %v", err)
	}

	state.close()
	state.close()

	if got := consoleGenerationRefs(jobGeneration); got != 0 {
		t.Fatalf("pending job generation refs = %d, want 0", got)
	}
	if got := consoleGenerationRefs(approvalGeneration); got != 0 {
		t.Fatalf("pending approval generation refs = %d, want 0", got)
	}
	if _, ok := state.takePendingJob("task_pending"); ok {
		t.Fatal("takePendingJob() found a job after close")
	}
	if _, ok := state.pendingApproval("approval_pending"); ok {
		t.Fatal("pendingApproval() found an approval after close")
	}
	select {
	case <-state.workersCtx.Done():
	default:
		t.Fatal("workers context remains active after close")
	}
	if err := state.addPendingJob(consoleLocalTaskJob{TaskID: "task_after_close"}); !errors.Is(err, errConsoleExecutionClosed) {
		t.Fatalf("addPendingJob() after close error = %v, want %v", err, errConsoleExecutionClosed)
	}
	if err := state.addPendingApproval("approval_after_close", consoleLocalTaskJob{}, time.Time{}); !errors.Is(err, errConsoleExecutionClosed) {
		t.Fatalf("addPendingApproval() after close error = %v, want %v", err, errConsoleExecutionClosed)
	}
}

func TestConsoleExecutionStateCloseCancelsPendingBusTask(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{Persist: false})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	const taskID = "task_pending_on_bus"
	if err := store.Upsert(daemonruntime.TaskInfo{
		ID:        taskID,
		Status:    daemonruntime.TaskQueued,
		Task:      "pending bus task",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("store.Upsert() error = %v", err)
	}
	runtime := &consoleLocalRuntime{store: store, streamHub: newConsoleStreamHub()}
	state := newConsoleExecutionState(nil, nil)
	state.onDrop = runtime.handleDroppedTaskJob
	generation := &consoleLocalRuntimeGeneration{}
	generation.acquire()
	if err := state.addPendingJob(consoleLocalTaskJob{TaskID: taskID, Generation: generation}); err != nil {
		t.Fatalf("addPendingJob() error = %v", err)
	}

	state.close()

	if got := consoleGenerationRefs(generation); got != 0 {
		t.Fatalf("pending bus generation refs = %d, want 0", got)
	}
	assertConsoleTaskCanceledOnRuntimeClose(t, store, taskID)
}

func TestConsoleExecutionStateClosePersistsPendingApprovalFailure(t *testing.T) {
	rootDir := t.TempDir()
	journalDir := t.TempDir()
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:    rootDir,
		JournalDir: journalDir,
		Persist:    true,
	})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	const (
		taskID     = "task_pending_approval_on_close"
		approvalID = "approval_pending_on_close"
	)
	pendingAt := time.Now().UTC()
	if err := store.Upsert(daemonruntime.TaskInfo{
		ID:                taskID,
		Status:            daemonruntime.TaskPending,
		Task:              "pending approval task",
		CreatedAt:         pendingAt,
		PendingAt:         &pendingAt,
		ApprovalRequestID: approvalID,
		Result:            map[string]any{"pending": true},
	}); err != nil {
		t.Fatalf("store.Upsert() error = %v", err)
	}
	runtime := &consoleLocalRuntime{store: store, streamHub: newConsoleStreamHub()}
	state := newConsoleExecutionState(runtime.expirePendingApproval, runtime.closePendingApproval)
	runtime.consoleExecutionState = state
	state.onDrop = runtime.handleDroppedTaskJob
	generation, approvalGuard := newConsoleApprovalGeneration(t, approvalID)
	generation.acquire()
	if err := state.addPendingApproval(approvalID, consoleLocalTaskJob{TaskID: taskID, Generation: generation}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("addPendingApproval() error = %v", err)
	}

	state.close()

	if got := consoleGenerationRefs(generation); got != 0 {
		t.Fatalf("pending approval generation refs = %d, want 0", got)
	}
	reloaded, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:    rootDir,
		JournalDir: journalDir,
		Persist:    true,
	})
	if err != nil {
		t.Fatalf("reload NewConsoleFileStore() error = %v", err)
	}
	task, ok := reloaded.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("reloaded.Get(%q) = %#v, %v", taskID, task, ok)
	}
	if task.Status != daemonruntime.TaskFailed || task.Error != consoleRuntimeClosedTaskError || task.FinishedAt == nil {
		t.Fatalf("task after close = %#v, want failed runtime-closed approval", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending approval fields after close = pending_at %v approval %q result %#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	record, ok, err := approvalGuard.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if !ok || record.Status != guard.ApprovalExpired || record.Actor != consoleApprovalShutdownActor || record.Comment != consoleApprovalShutdownComment {
		t.Fatalf("approval after close = %#v, %v; want shutdown expiry", record, ok)
	}
}

func TestConsoleExecutionStateConcurrentRegisterAndClosePreservesOwnership(t *testing.T) {
	const attempts = 200
	for i := 0; i < attempts; i++ {
		state := newConsoleExecutionState(nil, nil)
		jobGeneration := &consoleLocalRuntimeGeneration{}
		approvalGeneration := &consoleLocalRuntimeGeneration{}
		jobGeneration.acquire()
		approvalGeneration.acquire()
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(3)

		go func() {
			defer wg.Done()
			<-start
			if err := state.addPendingJob(consoleLocalTaskJob{TaskID: "task", Generation: jobGeneration}); err != nil {
				jobGeneration.release()
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if err := state.addPendingApproval("approval", consoleLocalTaskJob{TaskID: "task", Generation: approvalGeneration}, time.Now().Add(time.Hour)); err != nil {
				approvalGeneration.release()
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			state.close()
		}()

		close(start)
		wg.Wait()
		state.close()
		if got := consoleGenerationRefs(jobGeneration); got != 0 {
			t.Fatalf("attempt %d: pending job generation refs = %d, want 0", i, got)
		}
		if got := consoleGenerationRefs(approvalGeneration); got != 0 {
			t.Fatalf("attempt %d: pending approval generation refs = %d, want 0", i, got)
		}
	}
}

func TestConsoleSubmitTaskViaBusPublishFailureReturnsGenerationOwnership(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{Persist: false})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	generation := &consoleLocalRuntimeGeneration{
		reader: viper.New(),
		commonDeps: depsutil.CommonDependencies{
			ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
				return llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "test-model"}}, nil
			},
		},
	}
	rt := &consoleLocalRuntime{store: store}
	rt.consoleExecutionState = newConsoleExecutionState(rt.expirePendingApproval, rt.closePendingApproval)
	t.Cleanup(func() { rt.consoleExecutionState.close() })
	generation.acquire()

	_, ownershipTransferred, err := rt.submitTaskViaBus(
		context.Background(),
		generation,
		"test task",
		"",
		"",
		time.Minute,
		"",
		"",
		"",
		nil,
		daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit"},
	)
	if err == nil {
		t.Fatal("submitTaskViaBus() error = nil, want uninitialized bus error")
	}
	if ownershipTransferred {
		t.Fatal("ownershipTransferred = true, want generation ownership returned to caller")
	}
	if got := consoleGenerationRefs(generation); got != 1 {
		t.Fatalf("generation refs after publish failure = %d, want caller-owned ref", got)
	}
	rt.consoleExecutionState.mu.Lock()
	pendingJobs := len(rt.consoleExecutionState.pendingJobs)
	rt.consoleExecutionState.mu.Unlock()
	if pendingJobs != 0 {
		t.Fatalf("pending jobs after publish failure = %d, want 0", pendingJobs)
	}

	generation.release()
	if got := consoleGenerationRefs(generation); got != 0 {
		t.Fatalf("generation refs after caller release = %d, want 0", got)
	}
}

func TestConsoleSubmitTaskViaBusCloseOwnsTerminalStateAfterAdmission(t *testing.T) {
	runtime := newConsoleGenerationTestRuntime(t, t.TempDir(), t.TempDir())
	generation, err := runtime.captureGeneration()
	if err != nil {
		t.Fatalf("captureGeneration() error = %v", err)
	}
	publishStarted := make(chan struct{})
	releasePublish := make(chan struct{})
	runtime.beforeConsoleInboundPublish = func() {
		close(publishStarted)
		<-releasePublish
	}
	type submitResult struct {
		ownershipTransferred bool
		err                  error
	}
	result := make(chan submitResult, 1)
	go func() {
		_, ownershipTransferred, err := runtime.submitTaskViaBus(
			context.Background(),
			generation,
			"close during admission",
			"",
			"",
			time.Minute,
			"",
			"",
			"",
			nil,
			daemonruntime.TaskTrigger{Source: "ui", Event: "chat_submit"},
		)
		result <- submitResult{ownershipTransferred: ownershipTransferred, err: err}
	}()
	select {
	case <-publishStarted:
	case <-time.After(time.Second):
		t.Fatal("submit did not reach the publish boundary")
	}

	runtime.Close()
	close(releasePublish)
	select {
	case got := <-result:
		if got.err == nil {
			t.Fatal("submitTaskViaBus() error = nil after runtime close")
		}
		if !got.ownershipTransferred {
			t.Fatal("ownershipTransferred = false, want close owner to retain ownership")
		}
	case <-time.After(time.Second):
		t.Fatal("submit did not return after publish was released")
	}

	tasks := runtime.store.List(daemonruntime.TaskListOptions{Limit: 10})
	if len(tasks) != 1 {
		t.Fatalf("stored tasks = %#v, want one admitted task", tasks)
	}
	if tasks[0].Status != daemonruntime.TaskCanceled || tasks[0].Error != consoleRuntimeClosedTaskError {
		t.Fatalf("task after close/publish race = %#v, want close-owned canceled state", tasks[0])
	}
	if got := consoleGenerationRefs(generation); got != 0 {
		t.Fatalf("generation refs after close/publish race = %d, want 0", got)
	}
}

func TestConsoleExecutionStateInitializesExecutionResources(t *testing.T) {
	state := newConsoleExecutionState(nil, nil)
	if state.runner != nil {
		t.Fatal("runner is initialized before its runtime handler is available")
	}
	if state.runControl == nil {
		t.Fatal("run control is nil")
	}
	if state.workersCtx == nil {
		t.Fatal("workers context is nil")
	}
	if state.cancelWorkers == nil {
		t.Fatal("workers cancel function is nil")
	}
	if err := state.workersCtx.Err(); err != nil {
		t.Fatalf("workers context error = %v before close", err)
	}
	state.close()
	if !errors.Is(state.workersCtx.Err(), context.Canceled) {
		t.Fatalf("workers context error = %v after close, want context canceled", state.workersCtx.Err())
	}
}

func TestConsoleExecutionStateCloseRejectsRunnerEnqueue(t *testing.T) {
	state := newConsoleExecutionState(nil, nil)
	handled := make(chan struct{}, 1)
	state.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		state.workersCtx,
		make(chan struct{}, 1),
		1,
		func(context.Context, string, consoleLocalTaskJob) {
			handled <- struct{}{}
		},
		runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{},
	)
	state.close()

	err := state.runner.Enqueue(context.Background(), "console:closed", func(uint64) consoleLocalTaskJob {
		return consoleLocalTaskJob{TaskID: "task_after_close"}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runner.Enqueue() after close error = %v, want context canceled", err)
	}
	select {
	case <-handled:
		t.Fatal("runner handled a job after close")
	default:
	}
}

func TestConsoleExecutionStateCloseCancelsQueuedTaskAndReleasesGeneration(t *testing.T) {
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{Persist: false})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	const taskID = "task_queued_at_close"
	if err := store.Upsert(daemonruntime.TaskInfo{
		ID:        taskID,
		Status:    daemonruntime.TaskQueued,
		Task:      "queued task",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("store.Upsert() error = %v", err)
	}

	state := newConsoleExecutionState(nil, nil)
	runtime := &consoleLocalRuntime{
		store:                 store,
		streamHub:             newConsoleStreamHub(),
		consoleExecutionState: state,
	}
	generation := &consoleLocalRuntimeGeneration{}
	generation.acquire()
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	handled := make(chan struct{}, 1)
	state.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		state.workersCtx,
		sem,
		1,
		func(context.Context, string, consoleLocalTaskJob) {
			handled <- struct{}{}
		},
		runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{
			OnDrop: runtime.handleDroppedTaskJob,
		},
	)
	if err := state.runner.Enqueue(context.Background(), "console:queued", func(uint64) consoleLocalTaskJob {
		return consoleLocalTaskJob{TaskID: taskID, Generation: generation}
	}); err != nil {
		t.Fatalf("runner.Enqueue() error = %v", err)
	}

	state.close()

	if got := consoleGenerationRefs(generation); got != 0 {
		t.Fatalf("queued task generation refs after close = %d, want 0", got)
	}
	task, ok := store.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) = %#v, %v", taskID, task, ok)
	}
	if task.Status != daemonruntime.TaskCanceled || task.Error != "console runtime closed" || task.FinishedAt == nil {
		t.Fatalf("task after close = %#v, want canceled with terminal error", task)
	}
	select {
	case <-handled:
		t.Fatal("runner handled the queued task after close")
	default:
	}
}

func TestConsoleTaskPanicFailsOnlyQueuedAndRunningTasks(t *testing.T) {
	for _, tc := range []struct {
		status     daemonruntime.TaskStatus
		wantStatus daemonruntime.TaskStatus
		wantError  string
	}{
		{status: daemonruntime.TaskQueued, wantStatus: daemonruntime.TaskFailed, wantError: "conversation worker panicked"},
		{status: daemonruntime.TaskRunning, wantStatus: daemonruntime.TaskFailed, wantError: "conversation worker panicked"},
		{status: daemonruntime.TaskPending, wantStatus: daemonruntime.TaskPending, wantError: "original state"},
		{status: daemonruntime.TaskDone, wantStatus: daemonruntime.TaskDone, wantError: "original state"},
		{status: daemonruntime.TaskFailed, wantStatus: daemonruntime.TaskFailed, wantError: "original state"},
		{status: daemonruntime.TaskCanceled, wantStatus: daemonruntime.TaskCanceled, wantError: "original state"},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{Persist: false})
			if err != nil {
				t.Fatalf("NewConsoleFileStore() error = %v", err)
			}
			finishedAt := time.Now().UTC().Add(-time.Minute)
			const taskID = "console_panic_transition"
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

			state := newConsoleExecutionState(nil, nil)
			runtime := &consoleLocalRuntime{store: store, consoleExecutionState: state}
			lease, err := runtime.runControl.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
				Runtime:         "console",
				ConversationKey: "console:test",
				TaskID:          taskID,
			})
			if err != nil {
				t.Fatalf("StartLease() error = %v", err)
			}
			runtime.handleTaskJobPanic("console:test", consoleLocalTaskJob{TaskID: taskID})
			if probe, err := runtime.runControl.StartLease(context.Background(), time.Minute, runtimecontrol.ActiveRun{
				Runtime:         "console",
				ConversationKey: "console:test",
				TaskID:          "probe",
			}); err != nil {
				lease.Finish()
				t.Fatalf("StartLease() after panic error = %v", err)
			} else {
				probe.Finish()
			}
			state.close()

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

func TestConsoleLocalRuntimeCloseWaitsForActiveGenerationOwner(t *testing.T) {
	var cleanupCalls atomic.Int32
	generation := &consoleLocalRuntimeGeneration{
		memRuntime: runtimecore.MemoryRuntime{
			Cleanup: func() {
				cleanupCalls.Add(1)
			},
		},
	}
	generation.acquire()
	rt := &consoleLocalRuntime{generation: generation}
	rt.consoleExecutionState = newConsoleExecutionState(rt.expirePendingApproval, rt.closePendingApproval)

	rt.Close()

	generation.mu.Lock()
	retired, cleaned, refs := generation.retired, generation.cleaned, generation.refs
	generation.mu.Unlock()
	if !retired || cleaned || refs != 1 {
		t.Fatalf("generation after Close = retired %v, cleaned %v, refs %d; want true, false, 1", retired, cleaned, refs)
	}
	if got := cleanupCalls.Load(); got != 0 {
		t.Fatalf("cleanup calls before active owner release = %d, want 0", got)
	}

	generation.release()

	generation.mu.Lock()
	cleaned, refs = generation.cleaned, generation.refs
	generation.mu.Unlock()
	if !cleaned || refs != 0 {
		t.Fatalf("generation after active owner release = cleaned %v, refs %d; want true, 0", cleaned, refs)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls after active owner release = %d, want 1", got)
	}
}

func consoleGenerationRefs(generation *consoleLocalRuntimeGeneration) int {
	if generation == nil {
		return 0
	}
	generation.mu.Lock()
	defer generation.mu.Unlock()
	return generation.refs
}

func assertConsoleTaskCanceledOnRuntimeClose(t *testing.T, store daemonruntime.TaskReader, taskID string) {
	t.Helper()
	task, ok := store.Get(taskID)
	if !ok || task == nil {
		t.Fatalf("store.Get(%q) = %#v, %v", taskID, task, ok)
	}
	if task.Status != daemonruntime.TaskCanceled || task.Error != consoleRuntimeClosedTaskError || task.FinishedAt == nil {
		t.Fatalf("task after close = %#v, want canceled with terminal error", task)
	}
}
