package guard

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type approvalRequestTestAuditSink struct {
	err error
}

func (s approvalRequestTestAuditSink) Emit(context.Context, AuditEvent) error { return s.err }
func (approvalRequestTestAuditSink) Close() error                             { return nil }

type approvalRequestTestStore struct {
	record     ApprovalRecord
	resolveErr error
}

func (s *approvalRequestTestStore) Create(_ context.Context, rec ApprovalRecord) (string, error) {
	rec.ID = "approval-test"
	s.record = rec
	return rec.ID, nil
}

func (s *approvalRequestTestStore) Get(context.Context, string) (ApprovalRecord, bool, error) {
	return s.record, s.record.ID != "", nil
}

func (s *approvalRequestTestStore) Resolve(_ context.Context, id string, status ApprovalStatus, actor, comment string) error {
	if s.resolveErr != nil {
		return s.resolveErr
	}
	s.record.ID = id
	s.record.Status = status
	s.record.Actor = actor
	s.record.Comment = comment
	return nil
}

func (*approvalRequestTestStore) ConsumeApproved(context.Context, string) (ApprovalRecord, error) {
	return ApprovalRecord{}, ErrApprovalNotApproved
}

func TestRequestApprovalExpiresRecordWhenAuditFails(t *testing.T) {
	auditErr := errors.New("audit unavailable")
	root := t.TempDir()
	store, err := NewFileApprovalStore(
		filepath.Join(root, "approvals.json"),
		filepath.Join(root, "locks"),
	)
	if err != nil {
		t.Fatalf("NewFileApprovalStore() error = %v", err)
	}
	g := New(Config{Enabled: true, Approvals: ApprovalsConfig{Enabled: true}}, approvalRequestTestAuditSink{err: auditErr}, store)

	id, err := g.RequestApproval(context.Background(), Meta{RunID: "run-test"}, Action{
		Type:       ActionToolCallPre,
		Identity:   "call-test",
		ToolName:   "bash",
		ToolParams: map[string]any{"cmd": "true"},
	}, Result{RiskLevel: RiskHigh, Decision: DecisionRequireApproval}, "bash", nil)
	if id != "" {
		t.Fatalf("RequestApproval() id = %q, want empty id on audit failure", id)
	}
	if !errors.Is(err, auditErr) {
		t.Fatalf("RequestApproval() error = %v, want audit error", err)
	}
	state, err := store.loadState()
	if err != nil {
		t.Fatalf("loadState() error = %v", err)
	}
	if len(state.Records) != 1 {
		t.Fatalf("approval records = %d, want 1", len(state.Records))
	}
	for _, record := range state.Records {
		if record.Status != ApprovalExpired || record.ResolvedAt == nil {
			t.Fatalf("approval = %+v, want durably expired record", record)
		}
		if record.Actor != "system:audit_failure" || record.Comment != "approval request audit failed" {
			t.Fatalf("approval resolution = actor %q, comment %q", record.Actor, record.Comment)
		}
	}
}

func TestRequestApprovalJoinsAuditAndCompensationErrors(t *testing.T) {
	auditErr := errors.New("audit unavailable")
	compensationErr := errors.New("approval compensation unavailable")
	store := &approvalRequestTestStore{resolveErr: compensationErr}
	g := New(Config{Enabled: true, Approvals: ApprovalsConfig{Enabled: true}}, approvalRequestTestAuditSink{err: auditErr}, store)

	_, err := g.RequestApproval(context.Background(), Meta{RunID: "run-test"}, Action{
		Type:       ActionToolCallPre,
		Identity:   "call-test",
		ToolName:   "bash",
		ToolParams: map[string]any{"cmd": "true"},
	}, Result{RiskLevel: RiskHigh, Decision: DecisionRequireApproval}, "bash", nil)
	if !errors.Is(err, auditErr) || !errors.Is(err, compensationErr) {
		t.Fatalf("RequestApproval() error = %v, want joined audit and compensation errors", err)
	}
}
