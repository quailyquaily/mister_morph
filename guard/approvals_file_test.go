package guard

import (
	"context"
	"errors"
	"path/filepath"
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
