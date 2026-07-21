package core

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestPendingApprovalRegistryExpiresHandleOnce(t *testing.T) {
	t.Parallel()

	expired := make(chan string, 2)
	registry := NewPendingApprovalRegistry(func(claim PendingApprovalClaim[string]) {
		expired <- claim.ID + ":" + claim.Job
	})
	t.Cleanup(func() { registry.Close() })
	registry.Register("apr_1", "task_1", time.Now().Add(20*time.Millisecond))

	select {
	case got := <-expired:
		if got != "apr_1:task_1" {
			t.Fatalf("expired callback = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval expiry")
	}
	if _, ok := registry.Get("apr_1"); ok {
		t.Fatal("expired handle remained in registry")
	}
	select {
	case got := <-expired:
		t.Fatalf("expiry callback called twice: %q", got)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestPendingApprovalRegistryClaimStopsExpiry(t *testing.T) {
	t.Parallel()

	var expired atomic.Int32
	registry := NewPendingApprovalRegistry(func(PendingApprovalClaim[int]) {
		expired.Add(1)
	})
	t.Cleanup(func() { registry.Close() })
	registry.Register("apr_1", 42, time.Now().Add(20*time.Millisecond))
	claim, state, err := registry.Claim("apr_1")
	if err != nil || state != PendingApprovalClaimOwned || claim.Job != 42 {
		t.Fatalf("Claim() = %#v, %v, %v", claim, state, err)
	}
	registry.CompleteClaim(claim)
	time.Sleep(50 * time.Millisecond)
	if got := expired.Load(); got != 0 {
		t.Fatalf("expiry callbacks = %d, want 0", got)
	}
}

func TestPendingApprovalRegistryRejectsRegistrationAfterClose(t *testing.T) {
	registry := NewPendingApprovalRegistry[string](nil)
	registry.Close()

	if _, _, err := registry.Register("apr_closed", "task_closed", time.Now().Add(time.Hour)); !errors.Is(err, ErrPendingApprovalRegistryUnavailable) {
		t.Fatalf("Register() error = %v, want registry unavailable", err)
	}
	if _, state, err := registry.Claim("apr_closed"); state != PendingApprovalClaimMissing || !errors.Is(err, ErrPendingApprovalRegistryUnavailable) {
		t.Fatalf("Claim() state = %v, error = %v; want missing and registry unavailable", state, err)
	}
}

func TestPendingApprovalRegistryCloseReturnsApprovalIdentity(t *testing.T) {
	registry := NewPendingApprovalRegistry[string](nil)
	if _, _, err := registry.Register("apr_close", "task_close", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	entries := registry.Close()
	if len(entries) != 1 || entries[0].ID != "apr_close" || entries[0].Job != "task_close" {
		t.Fatalf("Close() entries = %#v, want approval and job identity", entries)
	}
}

func TestPendingApprovalRegistryRestoreClaimUsesRetryDeadline(t *testing.T) {
	expired := make(chan time.Time, 1)
	registry := NewPendingApprovalRegistry(func(PendingApprovalClaim[string]) {
		expired <- time.Now()
	})
	t.Cleanup(func() { registry.Close() })
	retryAt := time.Now().Add(80 * time.Millisecond)
	if _, _, err := registry.Register("apr_retry", "task_retry", time.Time{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	claim, state, err := registry.Claim("apr_retry")
	if err != nil || state != PendingApprovalClaimOwned {
		t.Fatalf("Claim() = %#v, %v, %v", claim, state, err)
	}
	if err := registry.RestoreClaim(claim, retryAt); err != nil {
		t.Fatalf("RestoreClaim() error = %v", err)
	}
	select {
	case firedAt := <-expired:
		t.Fatalf("retry fired early at %v before %v", firedAt, retryAt)
	case <-time.After(30 * time.Millisecond):
	}
	select {
	case firedAt := <-expired:
		if firedAt.Before(retryAt.Add(-10 * time.Millisecond)) {
			t.Fatalf("retry fired at %v before %v", firedAt, retryAt)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for restored approval retry")
	}
}

func TestPendingApprovalRegistryClosePreventsExpiryCallbackRestore(t *testing.T) {
	callbackStarted := make(chan struct{})
	allowRestore := make(chan struct{})
	restoreErr := make(chan error, 1)
	closeDone := make(chan struct{})
	restoreDone := make(chan struct{})
	allowReturn := make(chan struct{})
	var registry *PendingApprovalRegistry[string]
	registry = NewPendingApprovalRegistry(func(claim PendingApprovalClaim[string]) {
		close(callbackStarted)
		<-allowRestore
		restoreErr <- registry.RestoreClaim(claim, time.Now().Add(time.Hour))
		close(restoreDone)
		<-allowReturn
	})
	if _, _, err := registry.Register("apr_close_race", "task_close_race", time.Now().Add(20*time.Millisecond)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for expiry callback")
	}
	go func() {
		registry.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("Close() returned before the in-flight expiry callback completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(allowRestore)
	select {
	case <-restoreDone:
	case <-time.After(time.Second):
		t.Fatal("expiry callback did not attempt to restore its claim")
	}
	select {
	case <-closeDone:
		t.Fatal("Close() returned before the expiry callback returned")
	case <-time.After(30 * time.Millisecond):
	}
	close(allowReturn)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() did not return after the expiry callback completed")
	}
	select {
	case err := <-restoreErr:
		if !errors.Is(err, ErrPendingApprovalRegistryUnavailable) {
			t.Fatalf("restore error = %v, want registry unavailable", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for restore result")
	}
	if _, ok := registry.Get("apr_close_race"); ok {
		t.Fatal("closed registry was resurrected by expiry callback")
	}
}

func TestPendingApprovalRegistryClaimHasSingleOwner(t *testing.T) {
	registry := NewPendingApprovalRegistry[string](nil)
	t.Cleanup(func() { registry.Close() })
	if _, _, err := registry.Register("apr_claim", "task_claim", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	claim, state, err := registry.Claim("apr_claim")
	if err != nil || state != PendingApprovalClaimOwned || claim.Job != "task_claim" {
		t.Fatalf("first Claim() = %#v, %v, %v; want owned task", claim, state, err)
	}
	duplicate, state, err := registry.Claim("apr_claim")
	if err != nil || state != PendingApprovalClaimInFlight || duplicate.Job != "task_claim" {
		t.Fatalf("second Claim() = %#v, %v, %v; want in-flight task", duplicate, state, err)
	}
	if err := registry.RestoreClaim(claim, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("RestoreClaim() error = %v", err)
	}
	restored, state, err := registry.Claim("apr_claim")
	if err != nil || state != PendingApprovalClaimOwned || restored.Job != "task_claim" {
		t.Fatalf("Claim() after restore = %#v, %v, %v; want owned task", restored, state, err)
	}
	registry.CompleteClaim(restored)
	if _, state, err := registry.Claim("apr_claim"); err != nil || state != PendingApprovalClaimMissing {
		t.Fatalf("Claim() after completion = %v, %v; want missing", state, err)
	}
}

func TestPendingApprovalRegistryCloseWaitsForExplicitClaim(t *testing.T) {
	registry := NewPendingApprovalRegistry[string](nil)
	if _, _, err := registry.Register("apr_claim_close", "task_claim_close", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	claim, state, err := registry.Claim("apr_claim_close")
	if err != nil || state != PendingApprovalClaimOwned {
		t.Fatalf("Claim() = %#v, %v, %v", claim, state, err)
	}
	closeDone := make(chan struct{})
	go func() {
		registry.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("Close() returned before the explicit claim completed")
	case <-time.After(30 * time.Millisecond):
	}
	registry.CompleteClaim(claim)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() did not return after the explicit claim completed")
	}
}

func TestPendingApprovalRegistryRestoreAfterCloseReleasesExplicitClaim(t *testing.T) {
	registry := NewPendingApprovalRegistry[string](nil)
	if _, _, err := registry.Register("apr_claim_restore_close", "task_claim_restore_close", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	claim, state, err := registry.Claim("apr_claim_restore_close")
	if err != nil || state != PendingApprovalClaimOwned {
		t.Fatalf("Claim() = %#v, %v, %v", claim, state, err)
	}
	closeDone := make(chan struct{})
	go func() {
		registry.Close()
		close(closeDone)
	}()
	for {
		_, _, probeErr := registry.Claim("apr_close_probe")
		if errors.Is(probeErr, ErrPendingApprovalRegistryUnavailable) {
			break
		}
		if probeErr != nil {
			t.Fatalf("probe Claim() error = %v", probeErr)
		}
	}
	if err := registry.RestoreClaim(claim, time.Now().Add(time.Hour)); !errors.Is(err, ErrPendingApprovalRegistryUnavailable) {
		t.Fatalf("RestoreClaim() error = %v, want registry unavailable", err)
	}
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() remained blocked after RestoreClaim consumed the explicit claim")
	}
}

func TestPendingApprovalRegistryClaimCompletionIsIdempotent(t *testing.T) {
	registry := NewPendingApprovalRegistry[string](nil)
	if _, _, err := registry.Register("apr_restore", "task_restore", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	claim, state, err := registry.Claim("apr_restore")
	if err != nil || state != PendingApprovalClaimOwned {
		t.Fatalf("Claim() = %#v, %v, %v", claim, state, err)
	}
	if err := registry.RestoreClaim(claim, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("RestoreClaim() error = %v", err)
	}
	if err := registry.RestoreClaim(claim, time.Now().Add(time.Hour)); !errors.Is(err, ErrPendingApprovalClaimLost) {
		t.Fatalf("second RestoreClaim() error = %v, want claim lost", err)
	}
	registry.CompleteClaim(claim)
	registry.CompleteClaim(claim)

	restored, state, err := registry.Claim("apr_restore")
	if err != nil || state != PendingApprovalClaimOwned {
		t.Fatalf("Claim() after restore = %#v, %v, %v", restored, state, err)
	}
	registry.CompleteClaim(restored)
	registry.CompleteClaim(restored)
	closeDone := make(chan struct{})
	go func() {
		registry.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() blocked after idempotent claim completion")
	}
}

func TestPendingApprovalRegistryExpiryOwnsHandleUntilRestore(t *testing.T) {
	callbackStarted := make(chan struct{})
	allowRestore := make(chan struct{})
	restoreErr := make(chan error, 1)
	var registry *PendingApprovalRegistry[string]
	registry = NewPendingApprovalRegistry(func(claim PendingApprovalClaim[string]) {
		close(callbackStarted)
		<-allowRestore
		restoreErr <- registry.RestoreClaim(claim, time.Now().Add(time.Hour))
	})
	t.Cleanup(func() { registry.Close() })
	if _, _, err := registry.Register("apr_expiry_claim", "task_expiry_claim", time.Now().Add(20*time.Millisecond)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("expiry callback did not start")
	}
	duplicate, state, err := registry.Claim("apr_expiry_claim")
	if err != nil || state != PendingApprovalClaimInFlight || duplicate.Job != "task_expiry_claim" {
		t.Fatalf("Claim() during expiry = %#v, %v, %v; want in-flight", duplicate, state, err)
	}
	close(allowRestore)
	select {
	case err := <-restoreErr:
		if err != nil {
			t.Fatalf("RestoreClaim() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expiry callback did not restore claim")
	}
	owner, state, err := registry.Claim("apr_expiry_claim")
	if err != nil || state != PendingApprovalClaimOwned || owner.Job != "task_expiry_claim" {
		t.Fatalf("Claim() after expiry restore = %#v, %v, %v", owner, state, err)
	}
	registry.CompleteClaim(owner)
}

func TestFailPendingApprovalTaskOnlyMatchesPendingApproval(t *testing.T) {
	store := daemonruntime.NewMemoryStore(4)
	pendingAt := time.Now().UTC()
	if err := store.Upsert(daemonruntime.TaskInfo{
		ID:                "task_registration",
		Status:            daemonruntime.TaskPending,
		PendingAt:         &pendingAt,
		ApprovalRequestID: "apr_registration",
		Result:            map[string]any{"pending": true},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	applied, err := FailPendingApprovalTask(store, "task_registration", "apr_other", ApprovalRegistrationFailedTaskError)
	if err != nil || applied {
		t.Fatalf("non-matching failure = %v, %v; want false, nil", applied, err)
	}
	applied, err = FailPendingApprovalTask(store, "task_registration", "apr_registration", ApprovalRegistrationFailedTaskError)
	if err != nil || !applied {
		t.Fatalf("matching failure = %v, %v; want true, nil", applied, err)
	}
	task, ok := store.Get("task_registration")
	if !ok || task.Status != daemonruntime.TaskFailed || task.Error != ApprovalRegistrationFailedTaskError || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want terminal registration failure", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
}

func TestExpirePendingApprovalPersistsTerminalStateAndAudit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	approvalStore, err := guard.NewFileApprovalStore(
		filepath.Join(root, "guard_approvals.json"),
		filepath.Join(root, "locks"),
	)
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	id, err := approvalStore.Create(context.Background(), guard.ApprovalRecord{
		ID:         "apr_expire",
		RunID:      "task_expire",
		ActionType: guard.ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "hash",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	audit := &approvalExpiryAuditSink{}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, audit, approvalStore)
	store := daemonruntime.NewMemoryStore(16)
	pendingAt := time.Now().UTC()
	store.Upsert(daemonruntime.TaskInfo{
		ID:                "task_expire",
		Status:            daemonruntime.TaskPending,
		PendingAt:         &pendingAt,
		ApprovalRequestID: id,
		Result:            map[string]any{"pending": true},
	})

	if err := ExpirePendingApproval(context.Background(), g, store, id, "task_expire", "test:expiry"); err != nil {
		t.Fatalf("ExpirePendingApproval() error = %v", err)
	}
	rec, ok, err := g.GetApproval(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("GetApproval() ok=%v err=%v", ok, err)
	}
	if rec.Status != guard.ApprovalExpired || rec.Actor != "test:expiry" {
		t.Fatalf("approval = %+v, want expired", rec)
	}
	task, ok := store.Get("task_expire")
	if !ok || task.Status != daemonruntime.TaskCanceled || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want canceled terminal state", task)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("task pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
	if audit.expired.Load() != 1 {
		t.Fatalf("expired audit events = %d, want 1", audit.expired.Load())
	}
}

func TestExpirePendingApprovalTerminatesTaskWhenAuditWriteFails(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	approvalStore, err := guard.NewFileApprovalStore(filepath.Join(root, "approvals.json"), filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	id, err := approvalStore.Create(context.Background(), guard.ApprovalRecord{
		ID:         "apr_audit_failure",
		ActionType: guard.ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "hash",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, failingApprovalExpiryAuditSink{}, approvalStore)
	store := daemonruntime.NewMemoryStore(4)
	pendingAt := time.Now().UTC()
	store.Upsert(daemonruntime.TaskInfo{ID: "task_audit_failure", Status: daemonruntime.TaskPending, PendingAt: &pendingAt, ApprovalRequestID: id})

	if err := ExpirePendingApproval(context.Background(), g, store, id, "task_audit_failure", "test:expiry"); err == nil {
		t.Fatal("ExpirePendingApproval() error = nil, want audit error")
	}
	task, ok := store.Get("task_audit_failure")
	if !ok || task.Status != daemonruntime.TaskCanceled {
		t.Fatalf("task = %+v, want canceled despite audit error", task)
	}
}

type approvalExpiryUpdateErrorStore struct {
	daemonruntime.TaskUpdater
	err       error
	failures  int
	callCount int
}

func (s *approvalExpiryUpdateErrorStore) Update(id string, update func(*daemonruntime.TaskInfo)) error {
	s.callCount++
	if s.failures > 0 {
		s.failures--
		return s.err
	}
	return s.TaskUpdater.Update(id, update)
}

func TestExpirePendingApprovalFailsTaskWhenResolveRemainsPending(t *testing.T) {
	t.Parallel()

	resolveErr := errors.New("approval store unavailable")
	approvalStore := &approvalResolutionTestStore{
		record: guard.ApprovalRecord{
			ID:     "apr_resolve_pending",
			Status: guard.ApprovalPending,
		},
		resolveErr: resolveErr,
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, approvalStore)
	store := daemonruntime.NewMemoryStore(4)
	pendingAt := time.Now().UTC()
	if err := store.Upsert(daemonruntime.TaskInfo{
		ID:                "task_resolve_pending",
		Status:            daemonruntime.TaskPending,
		PendingAt:         &pendingAt,
		ApprovalRequestID: approvalStore.record.ID,
		Result:            map[string]any{"pending": true},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	err := ExpirePendingApproval(context.Background(), g, store, approvalStore.record.ID, "task_resolve_pending", "test:expiry")
	if err != resolveErr {
		t.Fatalf("ExpirePendingApproval() error = %v, want original resolve error", err)
	}
	task, ok := store.Get("task_resolve_pending")
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.Error != ApprovalExpiryResolutionFailedTaskError {
		t.Fatalf("task error = %q, want fixed safe error", task.Error)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
}

func TestExpirePendingApprovalFallsBackToFailedWhenCancelUpdateFails(t *testing.T) {
	t.Parallel()

	updateErr := errors.New("task cancel update unavailable")
	approvalStore := &approvalResolutionTestStore{record: guard.ApprovalRecord{
		ID:     "apr_cancel_update",
		Status: guard.ApprovalPending,
	}}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, approvalStore)
	baseStore := daemonruntime.NewMemoryStore(4)
	pendingAt := time.Now().UTC()
	if err := baseStore.Upsert(daemonruntime.TaskInfo{
		ID:                "task_cancel_update",
		Status:            daemonruntime.TaskPending,
		PendingAt:         &pendingAt,
		ApprovalRequestID: approvalStore.record.ID,
		Result:            map[string]any{"pending": true},
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	store := &approvalExpiryUpdateErrorStore{TaskUpdater: baseStore, err: updateErr, failures: 1}

	err := ExpirePendingApproval(context.Background(), g, store, approvalStore.record.ID, "task_cancel_update", "test:expiry")
	if !errors.Is(err, updateErr) {
		t.Fatalf("ExpirePendingApproval() error = %v, want cancel update error", err)
	}
	if store.callCount != 2 {
		t.Fatalf("Update() calls = %d, want cancel attempt plus fail-closed update", store.callCount)
	}
	task, ok := baseStore.Get("task_cancel_update")
	if !ok || task.Status != daemonruntime.TaskFailed || task.FinishedAt == nil {
		t.Fatalf("task = %+v, want failed", task)
	}
	if task.Error != ApprovalExpiryResolutionFailedTaskError {
		t.Fatalf("task error = %q, want fixed safe error", task.Error)
	}
	if task.PendingAt != nil || task.ApprovalRequestID != "" || task.Result != nil {
		t.Fatalf("pending fields = %v/%q/%#v, want cleared", task.PendingAt, task.ApprovalRequestID, task.Result)
	}
}

func TestExpirePendingApprovalReportsRetryableTaskFinalizationFailure(t *testing.T) {
	t.Parallel()

	updateErr := errors.New("task store unavailable")
	approvalStore := &approvalResolutionTestStore{record: guard.ApprovalRecord{
		ID:     "apr_retry_task_finalization",
		Status: guard.ApprovalPending,
	}}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, approvalStore)
	baseStore := daemonruntime.NewMemoryStore(4)
	pendingAt := time.Now().UTC()
	if err := baseStore.Upsert(daemonruntime.TaskInfo{
		ID:                "task_retry_task_finalization",
		Status:            daemonruntime.TaskPending,
		PendingAt:         &pendingAt,
		ApprovalRequestID: approvalStore.record.ID,
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	store := &approvalExpiryUpdateErrorStore{TaskUpdater: baseStore, err: updateErr, failures: 2}

	err := ExpirePendingApproval(context.Background(), g, store, approvalStore.record.ID, "task_retry_task_finalization", "test:expiry")
	if !errors.Is(err, ErrApprovalTaskFinalizationFailed) || !errors.Is(err, updateErr) {
		t.Fatalf("ExpirePendingApproval() error = %v, want retryable finalization and update errors", err)
	}
	record, ok, getErr := g.GetApproval(context.Background(), approvalStore.record.ID)
	if getErr != nil || !ok || record.Status != guard.ApprovalExpired {
		t.Fatalf("approval after failed task writes = %+v, ok=%v err=%v; want expired", record, ok, getErr)
	}
	task, ok := baseStore.Get("task_retry_task_finalization")
	if !ok || task.Status != daemonruntime.TaskPending || task.ApprovalRequestID != approvalStore.record.ID {
		t.Fatalf("task after failed writes = %+v, want matching pending task for retry", task)
	}
}

func TestExpirePendingApprovalDoesNotOverwriteNonMatchingTask(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		status     daemonruntime.TaskStatus
		approvalID string
	}{
		{name: "queued", status: daemonruntime.TaskQueued, approvalID: "apr_noop"},
		{name: "done", status: daemonruntime.TaskDone, approvalID: "apr_noop"},
		{name: "canceled", status: daemonruntime.TaskCanceled, approvalID: "apr_noop"},
		{name: "failed", status: daemonruntime.TaskFailed, approvalID: "apr_noop"},
		{name: "other approval", status: daemonruntime.TaskPending, approvalID: "apr_other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			approvalStore := &approvalResolutionTestStore{record: guard.ApprovalRecord{
				ID:     "apr_noop",
				Status: guard.ApprovalPending,
			}}
			g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, approvalStore)
			store := daemonruntime.NewMemoryStore(4)
			pendingAt := time.Now().UTC()
			if err := store.Upsert(daemonruntime.TaskInfo{
				ID:                "task_noop",
				Status:            tc.status,
				PendingAt:         &pendingAt,
				ApprovalRequestID: tc.approvalID,
				Error:             "unchanged",
				Result:            map[string]any{"unchanged": true},
			}); err != nil {
				t.Fatalf("Upsert() error = %v", err)
			}

			err := ExpirePendingApproval(context.Background(), g, store, approvalStore.record.ID, "task_noop", "test:expiry")
			if !errors.Is(err, ErrApprovalTaskStateUnchanged) {
				t.Fatalf("ExpirePendingApproval() error = %v, want unchanged sentinel", err)
			}
			task, ok := store.Get("task_noop")
			if !ok || task.Status != tc.status || task.ApprovalRequestID != tc.approvalID || task.Error != "unchanged" || task.FinishedAt != nil {
				t.Fatalf("task = %+v, want unchanged", task)
			}
			if task.PendingAt == nil || task.Result == nil {
				t.Fatalf("task pending fields changed: %+v", task)
			}
		})
	}
}

func TestExpirePendingApprovalReturnsUnchangedForMissingTask(t *testing.T) {
	t.Parallel()

	approvalStore := &approvalResolutionTestStore{record: guard.ApprovalRecord{
		ID:     "apr_missing_task",
		Status: guard.ApprovalPending,
	}}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, approvalStore)

	err := ExpirePendingApproval(context.Background(), g, daemonruntime.NewMemoryStore(4), approvalStore.record.ID, "missing", "test:expiry")
	if !errors.Is(err, ErrApprovalTaskStateUnchanged) {
		t.Fatalf("ExpirePendingApproval() error = %v, want unchanged sentinel", err)
	}
}

func TestExpirePendingApprovalFallbackDoesNotOverwriteQueuedTask(t *testing.T) {
	t.Parallel()

	updateErr := errors.New("task cancel update unavailable")
	approvalStore := &approvalResolutionTestStore{record: guard.ApprovalRecord{
		ID:     "apr_fallback_noop",
		Status: guard.ApprovalPending,
	}}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, approvalStore)
	baseStore := daemonruntime.NewMemoryStore(4)
	if err := baseStore.Upsert(daemonruntime.TaskInfo{
		ID:                "task_fallback_noop",
		Status:            daemonruntime.TaskQueued,
		ApprovalRequestID: approvalStore.record.ID,
		Error:             "unchanged",
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	store := &approvalExpiryUpdateErrorStore{TaskUpdater: baseStore, err: updateErr, failures: 1}

	err := ExpirePendingApproval(context.Background(), g, store, approvalStore.record.ID, "task_fallback_noop", "test:expiry")
	if !errors.Is(err, updateErr) || !errors.Is(err, ErrApprovalTaskStateUnchanged) {
		t.Fatalf("ExpirePendingApproval() error = %v, want update error and unchanged sentinel", err)
	}
	if store.callCount != 2 {
		t.Fatalf("Update() calls = %d, want 2", store.callCount)
	}
	task, ok := baseStore.Get("task_fallback_noop")
	if !ok || task.Status != daemonruntime.TaskQueued || task.Error != "unchanged" || task.FinishedAt != nil {
		t.Fatalf("task = %+v, want unchanged queued task", task)
	}
}

type approvalExpiryAuditSink struct {
	expired atomic.Int32
}

func (s *approvalExpiryAuditSink) Emit(_ context.Context, event guard.AuditEvent) error {
	if event.ApprovalStatus == string(guard.ApprovalExpired) {
		s.expired.Add(1)
	}
	return nil
}

func (*approvalExpiryAuditSink) Close() error { return nil }

type failingApprovalExpiryAuditSink struct{}

func (failingApprovalExpiryAuditSink) Emit(context.Context, guard.AuditEvent) error {
	return errors.New("audit unavailable")
}

func (failingApprovalExpiryAuditSink) Close() error { return nil }
