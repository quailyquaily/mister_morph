package guard

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFileApprovalStoreCreateGetResolve(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewFileApprovalStore(
		filepath.Join(root, "approvals", "guard_approvals.json"),
		filepath.Join(root, ".fslocks"),
	)
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}

	id, err := store.Create(context.Background(), ApprovalRecord{
		RunID:      "run-1",
		ActionType: ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "hash-1",
		RiskLevel:  RiskHigh,
		Decision:   DecisionRequireApproval,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if id == "" {
		t.Fatalf("Create() returned empty id")
	}

	rec, ok, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatalf("Get() expected ok=true")
	}
	if rec.Status != ApprovalPending {
		t.Fatalf("Get() status = %s, want %s", rec.Status, ApprovalPending)
	}

	if err := store.Resolve(context.Background(), id, ApprovalApproved, "tester", "ok"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	rec, ok, err = store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() after resolve error = %v", err)
	}
	if !ok {
		t.Fatalf("Get() after resolve expected ok=true")
	}
	if rec.Status != ApprovalApproved {
		t.Fatalf("Get() after resolve status = %s, want %s", rec.Status, ApprovalApproved)
	}
	if rec.Actor != "tester" {
		t.Fatalf("Get() after resolve actor = %q, want %q", rec.Actor, "tester")
	}

	if err := store.Resolve(context.Background(), id, ApprovalDenied, "other", "too late"); !errors.Is(err, ErrApprovalNotPending) {
		t.Fatalf("second Resolve() error = %v, want ErrApprovalNotPending", err)
	}

	rec, ok, err = store.Get(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("Get() after second resolve ok=%v err=%v", ok, err)
	}
	if rec.Status != ApprovalApproved || rec.Actor != "tester" || rec.Comment != "ok" {
		t.Fatalf("record after second resolve = %+v, want original approval", rec)
	}
}

func TestFileApprovalStoreResolveMissingReturnsError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewFileApprovalStore(
		filepath.Join(root, "approvals", "guard_approvals.json"),
		filepath.Join(root, ".fslocks"),
	)
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}

	if err := store.Resolve(context.Background(), "apr_missing", ApprovalApproved, "tester", "ok"); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrApprovalNotFound", err)
	}
}

func TestFileApprovalStoreResolvePendingAsExpired(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewFileApprovalStore(
		filepath.Join(root, "approvals", "guard_approvals.json"),
		filepath.Join(root, ".fslocks"),
	)
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	id, err := store.Create(context.Background(), ApprovalRecord{
		ActionType: ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "hash-expired",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := store.Resolve(context.Background(), id, ApprovalExpired, "system:expiry", "approval expired"); err != nil {
		t.Fatalf("Resolve(expired) error = %v", err)
	}
	rec, ok, err := store.Get(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("Get() ok=%v err=%v", ok, err)
	}
	if rec.Status != ApprovalExpired || rec.Actor != "system:expiry" || rec.ResolvedAt == nil {
		t.Fatalf("record = %+v, want expired resolution", rec)
	}
}

func TestFileApprovalStoreConsumesApprovedRecordAtomically(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewFileApprovalStore(
		filepath.Join(root, "approvals", "guard_approvals.json"),
		filepath.Join(root, ".fslocks"),
	)
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	id, err := store.Create(context.Background(), ApprovalRecord{
		ActionType: ActionToolCallPre,
		ToolName:   "bash",
		ActionHash: "hash-once",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := store.Resolve(context.Background(), id, ApprovalApproved, "tester", ""); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	const attempts = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	var consumed atomic.Int32
	var rejected atomic.Int32
	errorsSeen := make(chan error, attempts)
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.ConsumeApproved(context.Background(), id)
			switch {
			case err == nil:
				consumed.Add(1)
			case errors.Is(err, ErrApprovalAlreadyConsumed):
				rejected.Add(1)
			default:
				errorsSeen <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("ConsumeApproved() unexpected error = %v", err)
	}
	if consumed.Load() != 1 || rejected.Load() != attempts-1 {
		t.Fatalf("consume results = success:%d rejected:%d, want 1/%d", consumed.Load(), rejected.Load(), attempts-1)
	}
	rec, ok, err := store.Get(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("Get() found = %v, error = %v", ok, err)
	}
	if rec.ConsumedAt == nil {
		t.Fatal("ConsumedAt is nil")
	}
}
