package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
)

type approvalResolutionTestStore struct {
	record     guard.ApprovalRecord
	resolveErr error
	getErr     error
}

func (s *approvalResolutionTestStore) Create(_ context.Context, rec guard.ApprovalRecord) (string, error) {
	s.record = rec
	return rec.ID, nil
}

func (s *approvalResolutionTestStore) Get(context.Context, string) (guard.ApprovalRecord, bool, error) {
	if s.getErr != nil {
		return guard.ApprovalRecord{}, false, s.getErr
	}
	return s.record, true, nil
}

func (s *approvalResolutionTestStore) Resolve(_ context.Context, _ string, status guard.ApprovalStatus, actor, comment string) error {
	if s.resolveErr != nil {
		return s.resolveErr
	}
	now := time.Now().UTC()
	s.record.Status = status
	s.record.Actor = actor
	s.record.Comment = comment
	s.record.ResolvedAt = &now
	return nil
}

func (*approvalResolutionTestStore) ConsumeApproved(context.Context, string) (guard.ApprovalRecord, error) {
	return guard.ApprovalRecord{}, errors.New("not implemented")
}

type approvalResolutionFailingAudit struct {
	err error
}

func (s approvalResolutionFailingAudit) Emit(context.Context, guard.AuditEvent) error {
	return s.err
}

func (approvalResolutionFailingAudit) Close() error { return nil }

func TestResolveApprovalCommitClassifiesCommittedAuditFailure(t *testing.T) {
	t.Parallel()

	auditErr := errors.New("approval audit unavailable")
	for _, target := range []guard.ApprovalStatus{guard.ApprovalApproved, guard.ApprovalDenied} {
		t.Run(string(target), func(t *testing.T) {
			t.Parallel()
			store := &approvalResolutionTestStore{record: guard.ApprovalRecord{
				ID:     "apr_committed",
				Status: guard.ApprovalPending,
			}}
			g := guard.New(
				guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}},
				approvalResolutionFailingAudit{err: auditErr},
				store,
			)

			state, rec, err := ResolveApprovalCommit(context.Background(), g, store.record.ID, target, "tester", "")
			if state != ApprovalCommitCommitted {
				t.Fatalf("state = %v, want committed", state)
			}
			if rec.Status != target {
				t.Fatalf("record status = %q, want %q", rec.Status, target)
			}
			if !errors.Is(err, auditErr) {
				t.Fatalf("error = %v, want audit error", err)
			}
		})
	}
}

func TestResolveApprovalCommitClassifiesPendingPreCommitFailure(t *testing.T) {
	t.Parallel()

	resolveErr := errors.New("approval store unavailable")
	expiresAt := time.Now().UTC().Add(time.Hour)
	store := &approvalResolutionTestStore{
		record: guard.ApprovalRecord{
			ID:        "apr_pending",
			Status:    guard.ApprovalPending,
			ExpiresAt: expiresAt,
		},
		resolveErr: resolveErr,
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, store)

	state, rec, err := ResolveApprovalCommit(context.Background(), g, store.record.ID, guard.ApprovalApproved, "tester", "")
	if state != ApprovalCommitPending {
		t.Fatalf("state = %v, want pending", state)
	}
	if rec.Status != guard.ApprovalPending || !rec.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("record = %+v, want pending with original expiry", rec)
	}
	if !errors.Is(err, resolveErr) {
		t.Fatalf("error = %v, want resolve error", err)
	}
}

func TestResolveApprovalCommitClassifiesUnknownReadFailure(t *testing.T) {
	t.Parallel()

	resolveErr := errors.New("approval resolve unavailable")
	getErr := errors.New("approval read unavailable")
	store := &approvalResolutionTestStore{
		record:     guard.ApprovalRecord{ID: "apr_unknown", Status: guard.ApprovalPending},
		resolveErr: resolveErr,
		getErr:     getErr,
	}
	g := guard.New(guard.Config{Enabled: true, Approvals: guard.ApprovalsConfig{Enabled: true}}, nil, store)

	state, _, err := ResolveApprovalCommit(context.Background(), g, store.record.ID, guard.ApprovalApproved, "tester", "")
	if state != ApprovalCommitUnknown {
		t.Fatalf("state = %v, want unknown", state)
	}
	if !errors.Is(err, resolveErr) || !errors.Is(err, getErr) {
		t.Fatalf("error = %v, want resolve and read errors", err)
	}
	if !errors.Is(err, ErrApprovalCommitIndeterminate) {
		t.Fatalf("error = %v, want indeterminate commit marker", err)
	}
}
