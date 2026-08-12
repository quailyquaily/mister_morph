package consolecmd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

type consoleApprovalFailingAudit struct {
	err error
}

func (s consoleApprovalFailingAudit) Emit(context.Context, guard.AuditEvent) error {
	return s.err
}

func (consoleApprovalFailingAudit) Close() error { return nil }

type consoleApprovalResolveErrorStore struct {
	guard.ApprovalStore
	err              error
	getErr           error
	resolveAttempted bool
}

type consoleApprovalUpdateErrorStore struct {
	daemonruntime.TaskUpdater
	err       error
	failures  int
	callCount int
}

type consoleApprovalRetryTaskStore struct {
	daemonruntime.TaskUpdater
	err       error
	mu        sync.Mutex
	failures  int
	callCount int
}

func (s *consoleApprovalRetryTaskStore) Update(id string, update func(*daemonruntime.TaskInfo)) error {
	s.mu.Lock()
	s.callCount++
	if s.failures > 0 {
		s.failures--
		err := s.err
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return s.TaskUpdater.Update(id, update)
}

func (s *consoleApprovalRetryTaskStore) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

type consoleApprovalBlockingResolveStore struct {
	guard.ApprovalStore
	resolveStarted chan struct{}
	allowResolve   chan struct{}
	startOnce      sync.Once
	releaseOnce    sync.Once
}

func (s *consoleApprovalBlockingResolveStore) Resolve(ctx context.Context, id string, status guard.ApprovalStatus, actor, comment string) error {
	s.startOnce.Do(func() { close(s.resolveStarted) })
	<-s.allowResolve
	return s.ApprovalStore.Resolve(ctx, id, status, actor, comment)
}

func (s *consoleApprovalBlockingResolveStore) unblock() {
	s.releaseOnce.Do(func() { close(s.allowResolve) })
}

func (s *consoleApprovalUpdateErrorStore) Update(id string, update func(*daemonruntime.TaskInfo)) error {
	s.callCount++
	if s.failures > 0 {
		s.failures--
		return s.err
	}
	return s.TaskUpdater.Update(id, update)
}

func (s *consoleApprovalResolveErrorStore) Resolve(ctx context.Context, id string, status guard.ApprovalStatus, actor, comment string) error {
	if !s.resolveAttempted {
		s.resolveAttempted = true
		return s.err
	}
	return s.ApprovalStore.Resolve(ctx, id, status, actor, comment)
}

func (s *consoleApprovalResolveErrorStore) Get(ctx context.Context, id string) (guard.ApprovalRecord, bool, error) {
	if s.resolveAttempted && s.getErr != nil {
		err := s.getErr
		s.getErr = nil
		return guard.ApprovalRecord{}, false, err
	}
	return s.ApprovalStore.Get(ctx, id)
}

func TestConsoleApprovalAuditFailureTerminatesClaimedTask(t *testing.T) {
	auditErr := errors.New("approval audit unavailable")
	for _, tc := range []struct {
		name       string
		approved   bool
		wantStatus daemonruntime.TaskStatus
		wantRecord guard.ApprovalStatus
	}{
		{name: "approve fails closed", approved: true, wantStatus: daemonruntime.TaskFailed, wantRecord: guard.ApprovalApproved},
		{name: "deny cancels", approved: false, wantStatus: daemonruntime.TaskCanceled, wantRecord: guard.ApprovalDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixture(t, auditErr, nil, nil)

			var err error
			if tc.approved {
				_, err = rt.approveApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
					ApprovalRequestID: approvalID,
					Actor:             "console:test",
				})
			} else {
				_, err = rt.denyApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
					ApprovalRequestID: approvalID,
					Actor:             "console:test",
				})
			}
			if err != auditErr {
				t.Fatalf("decision error = %v, want original audit error", err)
			}

			task, ok := rt.store.Get(taskID)
			if !ok || task.Status != tc.wantStatus || task.FinishedAt == nil {
				t.Fatalf("task = %+v, want terminal status %q", task, tc.wantStatus)
			}
			wantApprovalID := ""
			if !tc.approved {
				wantApprovalID = approvalID
			}
			if task.PendingAt != nil || strings.TrimSpace(task.ApprovalRequestID) != wantApprovalID || task.Result != nil {
				t.Fatalf("approval fields = %v/%q/%#v, want approval reference %q", task.PendingAt, task.ApprovalRequestID, task.Result, wantApprovalID)
			}
			if _, ok := rt.pendingApproval(approvalID); ok {
				t.Fatal("claimed approval handle remained registered")
			}
			rec, ok, getErr := generation.bundle.taskRuntime.SharedGuard.GetApproval(context.Background(), approvalID)
			if getErr != nil || !ok || rec.Status != tc.wantRecord {
				t.Fatalf("approval = %+v, ok=%v err=%v; want %q", rec, ok, getErr, tc.wantRecord)
			}
			generation.mu.Lock()
			refs := generation.refs
			generation.mu.Unlock()
			if refs != 0 {
				t.Fatalf("generation refs = %d, want 0", refs)
			}
		})
	}
}

func TestConsoleConcurrentApprovalDecisionHasSingleOwner(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ownerApproved bool
		wantStatus    daemonruntime.TaskStatus
		wantApproval  guard.ApprovalStatus
	}{
		{name: "approve owner rejects concurrent deny", ownerApproved: true, wantStatus: daemonruntime.TaskQueued, wantApproval: guard.ApprovalApproved},
		{name: "deny owner rejects concurrent approve", ownerApproved: false, wantStatus: daemonruntime.TaskCanceled, wantApproval: guard.ApprovalDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blockingStore := &consoleApprovalBlockingResolveStore{
				resolveStarted: make(chan struct{}),
				allowResolve:   make(chan struct{}),
			}
			rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixtureWithStore(t, func(base guard.ApprovalStore) guard.ApprovalStore {
				blockingStore.ApprovalStore = base
				return blockingStore
			})
			t.Cleanup(blockingStore.unblock)

			workersCtx, cancelWorkers := context.WithCancel(context.Background())
			t.Cleanup(cancelWorkers)
			resumedJobs := make(chan consoleLocalTaskJob, 1)
			rt.runner = runtimecore.NewConversationRunner(
				workersCtx,
				make(chan struct{}, 1),
				1,
				func(_ context.Context, _ string, job consoleLocalTaskJob) {
					resumedJobs <- job
				},
				runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{},
			)

			type decisionResult struct {
				response daemonruntime.ApprovalDecisionResponse
				err      error
			}
			ownerResult := make(chan decisionResult, 1)
			go func() {
				req := daemonruntime.ApprovalDecisionRequest{ApprovalRequestID: approvalID, Actor: "console:owner"}
				var response daemonruntime.ApprovalDecisionResponse
				var err error
				if tc.ownerApproved {
					response, err = rt.approveApproval(context.Background(), req)
				} else {
					response, err = rt.denyApproval(context.Background(), req)
				}
				ownerResult <- decisionResult{response: response, err: err}
			}()

			select {
			case <-blockingStore.resolveStarted:
			case <-time.After(time.Second):
				t.Fatal("approval owner did not reach the guarded resolve")
			}

			duplicateReq := daemonruntime.ApprovalDecisionRequest{ApprovalRequestID: approvalID, Actor: "console:duplicate"}
			var duplicateErr error
			if tc.ownerApproved {
				_, duplicateErr = rt.denyApproval(context.Background(), duplicateReq)
			} else {
				_, duplicateErr = rt.approveApproval(context.Background(), duplicateReq)
			}
			if !errors.Is(duplicateErr, runtimecore.ErrPendingApprovalClaimInFlight) {
				t.Fatalf("concurrent decision error = %v, want claim in flight", duplicateErr)
			}
			task, ok := rt.store.Get(taskID)
			if !ok || task.Status != daemonruntime.TaskPending || task.ApprovalRequestID != approvalID {
				t.Fatalf("task while owner is in flight = %+v, want matching pending task", task)
			}
			if got := consoleGenerationRefs(generation); got != 1 {
				t.Fatalf("generation refs while owner is in flight = %d, want 1", got)
			}

			blockingStore.unblock()
			select {
			case result := <-ownerResult:
				if result.err != nil {
					t.Fatalf("owner decision error = %v", result.err)
				}
			case <-time.After(time.Second):
				t.Fatal("approval owner did not finish")
			}

			if tc.ownerApproved {
				select {
				case job := <-resumedJobs:
					if job.Generation != nil {
						job.Generation.release()
					}
				case <-time.After(time.Second):
					t.Fatal("approved owner did not enqueue the resume job")
				}
			}
			task, _ = rt.store.Get(taskID)
			if task.Status != tc.wantStatus {
				t.Fatalf("task status after owner decision = %q, want %q", task.Status, tc.wantStatus)
			}
			rec, found, err := generation.bundle.taskRuntime.SharedGuard.GetApproval(context.Background(), approvalID)
			if err != nil || !found || rec.Status != tc.wantApproval {
				t.Fatalf("approval after owner decision = %+v, found=%v err=%v; want %q", rec, found, err, tc.wantApproval)
			}
			if got := consoleGenerationRefs(generation); got != 0 {
				t.Fatalf("generation refs after owner decision = %d, want 0", got)
			}
		})
	}
}

func TestConsoleCloseWaitsForApprovalDecisionOwner(t *testing.T) {
	blockingStore := &consoleApprovalBlockingResolveStore{
		resolveStarted: make(chan struct{}),
		allowResolve:   make(chan struct{}),
	}
	rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixtureWithStore(t, func(base guard.ApprovalStore) guard.ApprovalStore {
		blockingStore.ApprovalStore = base
		return blockingStore
	})
	t.Cleanup(blockingStore.unblock)

	ownerResult := make(chan error, 1)
	go func() {
		_, err := rt.denyApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
			ApprovalRequestID: approvalID,
			Actor:             "console:owner",
		})
		ownerResult <- err
	}()
	select {
	case <-blockingStore.resolveStarted:
	case <-time.After(time.Second):
		t.Fatal("approval owner did not reach the guarded resolve")
	}

	closeResult := make(chan bool, 1)
	go func() {
		closeResult <- rt.consoleExecutionState.close()
	}()
	waitForConsoleApprovalState(t, time.Second, func() bool {
		rt.consoleExecutionState.mu.Lock()
		defer rt.consoleExecutionState.mu.Unlock()
		return rt.consoleExecutionState.closed
	})
	select {
	case <-closeResult:
		t.Fatal("close returned while the approval decision still owned its claim")
	case <-time.After(50 * time.Millisecond):
	}
	task, ok := rt.store.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskPending || task.ApprovalRequestID != approvalID {
		t.Fatalf("task while close waits = %+v, want matching pending task", task)
	}
	if got := consoleGenerationRefs(generation); got != 1 {
		t.Fatalf("generation refs while close waits = %d, want 1", got)
	}

	blockingStore.unblock()
	select {
	case err := <-ownerResult:
		if err != nil {
			t.Fatalf("owner decision error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("approval owner did not finish")
	}
	select {
	case closed := <-closeResult:
		if !closed {
			t.Fatal("first close reported that the execution state was already closed")
		}
	case <-time.After(time.Second):
		t.Fatal("close did not return after the approval owner finished")
	}

	task, _ = rt.store.Get(taskID)
	if task.Status != daemonruntime.TaskCanceled || task.FinishedAt == nil {
		t.Fatalf("task after owner decision = %+v, want canceled", task)
	}
	rec, found, err := generation.bundle.taskRuntime.SharedGuard.GetApproval(context.Background(), approvalID)
	if err != nil || !found || rec.Status != guard.ApprovalDenied {
		t.Fatalf("approval after owner decision = %+v, found=%v err=%v; want denied", rec, found, err)
	}
	if got := consoleGenerationRefs(generation); got != 0 {
		t.Fatalf("generation refs after close = %d, want 0", got)
	}
}

func TestConsoleApprovalPreCommitFailureRestoresHandleAndGeneration(t *testing.T) {
	resolveErr := errors.New("approval store unavailable")
	rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixture(t, nil, resolveErr, nil)

	_, err := rt.approveApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:test",
	})
	if !errors.Is(err, resolveErr) {
		t.Fatalf("approve error = %v, want resolve error", err)
	}
	job, ok := rt.pendingApproval(approvalID)
	if !ok || job.TaskID != taskID || job.Generation != generation {
		t.Fatalf("restored job = %+v, ok=%v", job, ok)
	}
	task, ok := rt.store.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskPending || task.ApprovalRequestID != approvalID {
		t.Fatalf("task = %+v, want pending", task)
	}
	rec, ok, getErr := generation.bundle.taskRuntime.SharedGuard.GetApproval(context.Background(), approvalID)
	if getErr != nil || !ok || rec.Status != guard.ApprovalPending {
		t.Fatalf("approval = %+v, ok=%v err=%v; want pending", rec, ok, getErr)
	}
	generation.mu.Lock()
	refs := generation.refs
	generation.mu.Unlock()
	if refs != 1 {
		t.Fatalf("generation refs = %d, want restored handle to retain one ref", refs)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, _ = rt.store.Get(taskID)
		if task.Status == daemonruntime.TaskCanceled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task.Status != daemonruntime.TaskCanceled {
		t.Fatalf("task status after restored timer = %q, want canceled", task.Status)
	}
	if _, ok := rt.pendingApproval(approvalID); ok {
		t.Fatal("restored approval handle remained after expiry")
	}
	generation.mu.Lock()
	refs = generation.refs
	generation.mu.Unlock()
	if refs != 0 {
		t.Fatalf("generation refs after expiry = %d, want 0", refs)
	}
}

func TestConsoleApprovalUnknownOutcomeFailsClosed(t *testing.T) {
	resolveErr := errors.New("approval store unavailable")
	getErr := errors.New("approval read unavailable")
	rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixture(t, nil, resolveErr, getErr)

	_, err := rt.approveApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:test",
	})
	if !errors.Is(err, resolveErr) || !errors.Is(err, getErr) {
		t.Fatalf("approve error = %v, want resolve and read errors", err)
	}
	task, ok := rt.store.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	if _, ok := rt.pendingApproval(approvalID); ok {
		t.Fatal("unknown approval handle remained registered")
	}
	generation.mu.Lock()
	refs := generation.refs
	generation.mu.Unlock()
	if refs != 0 {
		t.Fatalf("generation refs = %d, want 0", refs)
	}
}

func TestConsoleApprovalQueuedUpdateFailureFailsClosed(t *testing.T) {
	updateErr := errors.New("queue task update unavailable")
	rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixture(t, nil, nil, nil)
	updater := &consoleApprovalUpdateErrorStore{TaskUpdater: rt.store, err: updateErr, failures: 1}
	rt.taskUpdater = updater
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	t.Cleanup(cancelRunner)
	rt.runner = runtimecore.NewConversationRunner(
		runnerCtx,
		make(chan struct{}, 1),
		1,
		func(_ context.Context, _ string, job consoleLocalTaskJob) {
			if job.Generation != nil {
				job.Generation.release()
			}
		},
		runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{},
	)

	_, err := rt.approveApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:test",
	})
	if err != updateErr {
		t.Fatalf("approve error = %v, want original update error", err)
	}
	if updater.callCount != 2 {
		t.Fatalf("task update calls = %d, want queued attempt plus fail-closed update", updater.callCount)
	}
	task, ok := rt.store.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	if _, ok := rt.pendingApproval(approvalID); ok {
		t.Fatal("approval handle remained registered")
	}
	rec, ok, getErr := generation.bundle.taskRuntime.SharedGuard.GetApproval(context.Background(), approvalID)
	if getErr != nil || !ok || rec.Status != guard.ApprovalApproved {
		t.Fatalf("approval = %+v, ok=%v err=%v; want approved", rec, ok, getErr)
	}
	generation.mu.Lock()
	refs := generation.refs
	generation.mu.Unlock()
	if refs != 0 {
		t.Fatalf("generation refs = %d, want 0", refs)
	}
}

func TestConsoleExpiredApprovalPreCommitFailureFailsClosed(t *testing.T) {
	resolveErr := errors.New("approval store unavailable")
	rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixtureAt(t, nil, resolveErr, nil, time.Now().UTC().Add(-time.Minute))

	_, err := rt.approveApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:test",
	})
	if err != resolveErr {
		t.Fatalf("approve error = %v, want original resolve error", err)
	}
	task, ok := rt.store.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	if _, ok := rt.pendingApproval(approvalID); ok {
		t.Fatal("expired approval handle remained registered")
	}
	generation.mu.Lock()
	refs := generation.refs
	generation.mu.Unlock()
	if refs != 0 {
		t.Fatalf("generation refs = %d, want 0", refs)
	}
}

func TestConsoleExpiredApprovalIndeterminateClickFailsMatchingTask(t *testing.T) {
	resolveErr := errors.New("approval store unavailable")
	getErr := errors.New("approval read unavailable")
	rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixtureAt(t, nil, resolveErr, getErr, time.Now().UTC().Add(-time.Minute))

	_, err := rt.approveApproval(context.Background(), daemonruntime.ApprovalDecisionRequest{
		ApprovalRequestID: approvalID,
		Actor:             "console:test",
	})
	if !errors.Is(err, resolveErr) || !errors.Is(err, getErr) {
		t.Fatalf("approve error = %v, want resolve and read errors", err)
	}
	task, ok := rt.store.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	generation.mu.Lock()
	refs := generation.refs
	generation.mu.Unlock()
	if refs != 0 {
		t.Fatalf("generation refs = %d, want 0", refs)
	}
}

func TestConsoleExpiryRetryRestoresClaimedHandleLease(t *testing.T) {
	resolveErr := errors.New("approval store unavailable")
	getErr := errors.New("approval read unavailable")
	rt, generation, approvalID, _ := newConsoleApprovalResolutionFixture(t, nil, resolveErr, getErr)
	claim, state, err := rt.claimPendingApproval(approvalID)
	if err != nil || state != runtimecore.PendingApprovalClaimOwned {
		t.Fatalf("claimPendingApproval() state=%v err=%v, want owned", state, err)
	}
	rt.expirePendingApproval(claim)

	generation.mu.Lock()
	refs := generation.refs
	generation.mu.Unlock()
	if refs != 1 {
		t.Fatalf("generation refs = %d, want only the existing handle lease", refs)
	}
	registered, ok := rt.pendingApproval(approvalID)
	if !ok || registered.TaskID != claim.Job.TaskID {
		t.Fatalf("registered job = %+v, ok=%v; want original handle", registered, ok)
	}
}

func TestConsoleExpiryRetriesFailedTaskFinalization(t *testing.T) {
	rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixture(t, nil, nil, nil)
	retryStore := &consoleApprovalRetryTaskStore{
		TaskUpdater: rt.store,
		err:         errors.New("task store unavailable"),
		failures:    2,
	}
	rt.taskUpdater = retryStore
	claim, state, err := rt.claimPendingApproval(approvalID)
	if err != nil || state != runtimecore.PendingApprovalClaimOwned {
		t.Fatalf("claimPendingApproval() state=%v err=%v, want owned", state, err)
	}

	rt.expirePendingApproval(claim)

	waitForConsoleApprovalState(t, 2*time.Second, func() bool {
		task, ok := rt.store.Get(taskID)
		return ok && task.Status == daemonruntime.TaskCanceled
	})
	if got := retryStore.calls(); got < 3 {
		t.Fatalf("task update calls = %d, want initial writes plus retry", got)
	}
	if _, ok := rt.pendingApproval(approvalID); ok {
		t.Fatal("approval handle remained registered after successful retry")
	}
	if got := consoleGenerationRefs(generation); got != 0 {
		t.Fatalf("generation refs = %d, want 0 after successful retry", got)
	}
}

func TestConsoleCloseFailsUnclaimedPendingApproval(t *testing.T) {
	rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixture(t, nil, nil, nil)
	approvalGuard := approvalGuardForGeneration(generation)

	rt.consoleExecutionState.close()

	task, ok := rt.store.Get(taskID)
	if !ok || task.Status != daemonruntime.TaskFailed || task.Error != consoleRuntimeClosedTaskError || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed after runtime close", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	if _, ok := rt.pendingApproval(approvalID); ok {
		t.Fatal("approval handle remained registered after close")
	}
	if got := consoleGenerationRefs(generation); got != 0 {
		t.Fatalf("generation refs = %d, want 0", got)
	}
	record, ok, err := approvalGuard.GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if record.Status != guard.ApprovalExpired || record.Actor != "system:console_shutdown" || record.Comment != "console runtime closed before approval decision" {
		t.Fatalf("approval after close = %+v, want shutdown expiry", record)
	}
}

func TestConsoleCloseDoesNotOverwriteResolvedApproval(t *testing.T) {
	rt, generation, approvalID, _ := newConsoleApprovalResolutionFixture(t, nil, nil, nil)
	approvalGuard := approvalGuardForGeneration(generation)
	if err := approvalGuard.ResolveApproval(context.Background(), approvalID, guard.ApprovalApproved, "tester", "approved first"); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	rt.consoleExecutionState.close()

	record, ok, err := approvalGuard.GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if record.Status != guard.ApprovalApproved || record.Actor != "tester" || record.Comment != "approved first" {
		t.Fatalf("approval after close = %+v, want original approved record", record)
	}
}

func TestConsoleRegisterPendingApprovalReturnsClosedRegistryErrorWithoutLeakingLease(t *testing.T) {
	rt, generation, approvalID, taskID := newConsoleApprovalResolutionFixture(t, nil, nil, nil)
	rt.consoleExecutionState.close()

	err := rt.registerPendingApproval(approvalID, consoleLocalTaskJob{TaskID: taskID, Generation: generation})
	if !errors.Is(err, runtimecore.ErrPendingApprovalRegistryUnavailable) {
		t.Fatalf("register error = %v, want registry unavailable", err)
	}
	generation.mu.Lock()
	refs := generation.refs
	generation.mu.Unlock()
	if refs != 0 {
		t.Fatalf("generation refs = %d, want 0 after failed registration", refs)
	}
}

func TestConsoleRegisterPendingApprovalRejectsUnavailableApprovalRecord(t *testing.T) {
	getErr := errors.New("approval read unavailable")
	root := t.TempDir()
	emptyStore, err := guard.NewFileApprovalStore(filepath.Join(root, "approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	tests := []struct {
		name    string
		guard   *guard.Guard
		wantErr error
	}{
		{name: "guard unavailable", wantErr: nil},
		{
			name: "approval read fails",
			guard: guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, &consoleApprovalResolveErrorStore{
				getErr:           getErr,
				resolveAttempted: true,
			}),
			wantErr: getErr,
		},
		{
			name:    "approval is missing",
			guard:   guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, emptyStore),
			wantErr: guard.ErrApprovalNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			generation := &consoleLocalRuntimeGeneration{bundle: &consoleLocalRuntimeBundle{taskRuntime: &taskruntime.Runtime{SharedGuard: tc.guard}}}
			rt := &consoleLocalRuntime{}
			rt.consoleExecutionState = newConsoleExecutionState(rt.expirePendingApproval, rt.closePendingApproval)
			err := rt.registerPendingApproval("apr_missing", consoleLocalTaskJob{TaskID: "task_missing", Generation: generation})
			if tc.wantErr == nil {
				if err == nil || !strings.Contains(err.Error(), "approvals are unavailable") {
					t.Fatalf("register error = %v, want approvals unavailable", err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("register error = %v, want %v", err, tc.wantErr)
			}
			generation.mu.Lock()
			refs := generation.refs
			generation.mu.Unlock()
			if refs != 0 {
				t.Fatalf("generation refs = %d, want 0", refs)
			}
		})
	}
}

func newConsoleApprovalResolutionFixture(t *testing.T, auditErr, resolveErr, getErr error) (*consoleLocalRuntime, *consoleLocalRuntimeGeneration, string, string) {
	return newConsoleApprovalResolutionFixtureAt(t, auditErr, resolveErr, getErr, time.Time{})
}

func newConsoleApprovalResolutionFixtureAt(t *testing.T, auditErr, resolveErr, getErr error, expiresAt time.Time) (*consoleLocalRuntime, *consoleLocalRuntimeGeneration, string, string) {
	return newConsoleApprovalResolutionFixtureWithStoreAt(t, auditErr, resolveErr, getErr, expiresAt, nil)
}

func newConsoleApprovalResolutionFixtureWithStore(t *testing.T, wrapStore func(guard.ApprovalStore) guard.ApprovalStore) (*consoleLocalRuntime, *consoleLocalRuntimeGeneration, string, string) {
	return newConsoleApprovalResolutionFixtureWithStoreAt(t, nil, nil, nil, time.Time{}, wrapStore)
}

func newConsoleApprovalResolutionFixtureWithStoreAt(t *testing.T, auditErr, resolveErr, getErr error, expiresAt time.Time, wrapStore func(guard.ApprovalStore) guard.ApprovalStore) (*consoleLocalRuntime, *consoleLocalRuntimeGeneration, string, string) {
	t.Helper()
	taskStore, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{Persist: false})
	if err != nil {
		t.Fatalf("NewConsoleFileStore() error = %v", err)
	}
	root := t.TempDir()
	baseStore, err := guard.NewFileApprovalStore(filepath.Join(root, "approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(time.Hour)
		if resolveErr != nil {
			expiresAt = time.Now().UTC().Add(time.Second)
		}
	}
	approvalID, err := baseStore.Create(context.Background(), guard.ApprovalRecord{
		ID:                    "apr_console_resolution",
		RunID:                 "task_console_resolution",
		ExpiresAt:             expiresAt,
		ActionType:            guard.ActionToolCallPre,
		ToolName:              "bash",
		ActionHash:            "hash",
		RiskLevel:             guard.RiskHigh,
		Decision:              guard.DecisionRequireApproval,
		ActionSummaryRedacted: "ToolCallPre tool=bash",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	var approvalStore guard.ApprovalStore = baseStore
	if wrapStore != nil {
		approvalStore = wrapStore(baseStore)
	} else if resolveErr != nil {
		approvalStore = &consoleApprovalResolveErrorStore{ApprovalStore: baseStore, err: resolveErr, getErr: getErr}
	}
	var audit guard.AuditSink
	if auditErr != nil {
		audit = consoleApprovalFailingAudit{err: auditErr}
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, audit, approvalStore)
	generation := &consoleLocalRuntimeGeneration{
		bundle: &consoleLocalRuntimeBundle{taskRuntime: &taskruntime.Runtime{SharedGuard: g}},
	}
	rt := &consoleLocalRuntime{
		store:      taskStore,
		generation: generation,
		streamHub:  newConsoleStreamHub(),
	}
	rt.consoleExecutionState = newConsoleExecutionState(rt.expirePendingApproval, rt.closePendingApproval)
	t.Cleanup(rt.Close)
	pendingAt := time.Now().UTC()
	taskID := "task_console_resolution"
	if err := taskStore.Upsert(daemonruntime.TaskInfo{
		ID:                taskID,
		Status:            daemonruntime.TaskPending,
		CreatedAt:         pendingAt,
		PendingAt:         &pendingAt,
		ApprovalRequestID: approvalID,
		Result:            map[string]any{"status": "pending"},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	job := consoleLocalTaskJob{
		TaskID:          taskID,
		ConversationKey: "console:test",
		Generation:      generation,
	}
	generation.acquire()
	handleExpiresAt := expiresAt
	if !handleExpiresAt.After(time.Now()) {
		handleExpiresAt = time.Now().UTC().Add(time.Hour)
	}
	if err := rt.consoleExecutionState.addPendingApproval(approvalID, job, handleExpiresAt); err != nil {
		t.Fatalf("addPendingApproval() error = %v", err)
	}
	if _, ok := rt.pendingApproval(approvalID); !ok {
		t.Fatal("approval handle was not registered")
	}
	return rt, generation, approvalID, taskID
}
