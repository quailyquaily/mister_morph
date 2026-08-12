package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

const (
	ApprovalExpiredTaskError                = "Approval expired. Task canceled."
	ApprovalExpiryResolutionFailedTaskError = "Approval expiration could not be completed. Task failed."
	ApprovalRegistrationFailedTaskError     = "Approval handle could not be registered. Task failed."
	PendingApprovalRetryDelay               = time.Second
)

var (
	ErrApprovalTaskStateUnchanged         = errors.New("approval task state was not updated")
	ErrApprovalTaskFinalizationFailed     = errors.New("approval task finalization failed")
	ErrApprovalCommitIndeterminate        = errors.New("approval commit state is indeterminate")
	ErrPendingApprovalRegistryUnavailable = errors.New("pending approval registry is unavailable")
	ErrPendingApprovalClaimInFlight       = errors.New("approval decision is already in progress")
	ErrPendingApprovalClaimLost           = errors.New("pending approval claim is no longer owned")
)

type ApprovalCommitState uint8

const (
	ApprovalCommitUnknown ApprovalCommitState = iota
	ApprovalCommitCommitted
	ApprovalCommitPending
)

type pendingApprovalEntry[J any] struct {
	job   J
	timer *time.Timer
}

type PendingApprovalHandle[J any] struct {
	ID  string
	Job J
}

type PendingApprovalClaimState uint8

const (
	PendingApprovalClaimMissing PendingApprovalClaimState = iota
	PendingApprovalClaimOwned
	PendingApprovalClaimInFlight
)

type PendingApprovalClaim[J any] struct {
	ID    string
	Job   J
	owner *pendingApprovalClaimOwner[J]
}

type pendingApprovalClaimOwner[J any] struct {
	registry *PendingApprovalRegistry[J]
	id       string
	entry    *pendingApprovalEntry[J]
	done     sync.Once
}

// PendingApprovalRegistry owns pending handles, their expiry timers, and the
// claims that temporarily remove handles while a decision is in progress.
type PendingApprovalRegistry[J any] struct {
	mu         sync.Mutex
	entries    map[string]*pendingApprovalEntry[J]
	claims     map[string]*pendingApprovalClaimOwner[J]
	onExpire   func(PendingApprovalClaim[J])
	ownerWG    sync.WaitGroup
	callbackWG sync.WaitGroup
	closed     bool
}

func NewPendingApprovalRegistry[J any](onExpire func(PendingApprovalClaim[J])) *PendingApprovalRegistry[J] {
	return &PendingApprovalRegistry[J]{
		entries:  make(map[string]*pendingApprovalEntry[J]),
		claims:   make(map[string]*pendingApprovalClaimOwner[J]),
		onExpire: onExpire,
	}
}

// Register stores a handle and schedules it from the approval record's expiry.
// It returns the displaced job when the same approval ID was already registered.
func (r *PendingApprovalRegistry[J]) Register(id string, job J, expiresAt time.Time) (J, bool, error) {
	var zero J
	if r == nil {
		return zero, false, ErrPendingApprovalRegistryUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return zero, false, fmt.Errorf("approval id is required")
	}

	entry := &pendingApprovalEntry[J]{job: job}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return zero, false, ErrPendingApprovalRegistryUnavailable
	}
	if r.entries == nil {
		r.entries = make(map[string]*pendingApprovalEntry[J])
	}
	if r.claims[id] != nil {
		r.mu.Unlock()
		return zero, false, ErrPendingApprovalClaimInFlight
	}
	previous, replaced := r.entries[id]
	if previous != nil && previous.timer != nil {
		previous.timer.Stop()
	}
	if !expiresAt.IsZero() {
		delay := time.Until(expiresAt)
		if delay < 0 {
			delay = 0
		}
		entry.timer = time.AfterFunc(delay, func() {
			r.expire(id, entry)
		})
	}
	r.entries[id] = entry
	r.mu.Unlock()
	if !replaced || previous == nil {
		return zero, false, nil
	}
	return previous.job, true, nil
}

func (r *PendingApprovalRegistry[J]) Claim(id string) (PendingApprovalClaim[J], PendingApprovalClaimState, error) {
	var zero PendingApprovalClaim[J]
	if r == nil {
		return zero, PendingApprovalClaimMissing, ErrPendingApprovalRegistryUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return zero, PendingApprovalClaimMissing, fmt.Errorf("approval id is required")
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return zero, PendingApprovalClaimMissing, ErrPendingApprovalRegistryUnavailable
	}
	if owner := r.claims[id]; owner != nil && owner.entry != nil {
		claim := PendingApprovalClaim[J]{ID: id, Job: owner.entry.job}
		r.mu.Unlock()
		return claim, PendingApprovalClaimInFlight, nil
	}
	entry := r.entries[id]
	if entry == nil {
		r.mu.Unlock()
		return zero, PendingApprovalClaimMissing, nil
	}
	delete(r.entries, id)
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	if r.claims == nil {
		r.claims = make(map[string]*pendingApprovalClaimOwner[J])
	}
	owner := &pendingApprovalClaimOwner[J]{registry: r, id: id, entry: entry}
	r.claims[id] = owner
	r.ownerWG.Add(1)
	claim := PendingApprovalClaim[J]{ID: id, Job: entry.job, owner: owner}
	r.mu.Unlock()
	return claim, PendingApprovalClaimOwned, nil
}

func (r *PendingApprovalRegistry[J]) CompleteClaim(claim PendingApprovalClaim[J]) {
	if r == nil || claim.owner == nil || claim.owner.registry != r {
		return
	}
	claim.owner.done.Do(func() {
		r.mu.Lock()
		if r.claims[claim.owner.id] == claim.owner {
			delete(r.claims, claim.owner.id)
		}
		r.mu.Unlock()
		r.ownerWG.Done()
	})
}

func (r *PendingApprovalRegistry[J]) RestoreClaim(claim PendingApprovalClaim[J], retryAt time.Time) error {
	if r == nil {
		return ErrPendingApprovalRegistryUnavailable
	}
	if claim.owner == nil || claim.owner.registry != r || claim.owner.entry == nil {
		return ErrPendingApprovalClaimLost
	}
	var restoreErr error
	restored := false
	claim.owner.done.Do(func() {
		restored = true
		defer r.ownerWG.Done()
		r.mu.Lock()
		defer r.mu.Unlock()
		id := claim.owner.id
		if r.claims[id] != claim.owner {
			restoreErr = ErrPendingApprovalClaimLost
			return
		}
		delete(r.claims, id)
		if r.closed {
			restoreErr = ErrPendingApprovalRegistryUnavailable
			return
		}
		entry := claim.owner.entry
		if !retryAt.IsZero() {
			delay := time.Until(retryAt)
			if delay < 0 {
				delay = 0
			}
			entry.timer = time.AfterFunc(delay, func() {
				r.expire(id, entry)
			})
		}
		if r.entries == nil {
			r.entries = make(map[string]*pendingApprovalEntry[J])
		}
		r.entries[id] = entry
	})
	if !restored {
		return ErrPendingApprovalClaimLost
	}
	return restoreErr
}

func (r *PendingApprovalRegistry[J]) Get(id string) (J, bool) {
	var zero J
	if r == nil {
		return zero, false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return zero, false
	}
	r.mu.Lock()
	entry, ok := r.entries[id]
	r.mu.Unlock()
	if !ok || entry == nil {
		return zero, false
	}
	return entry.job, true
}

// Close stops all timers and returns the approval identities and jobs whose
// ownership remains with the caller, allowing durable state and resources to
// be finalized together.
func (r *PendingApprovalRegistry[J]) Close() []PendingApprovalHandle[J] {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.closed = true
	handles := make([]PendingApprovalHandle[J], 0, len(r.entries))
	for id, entry := range r.entries {
		if entry != nil {
			if entry.timer != nil {
				entry.timer.Stop()
			}
			handles = append(handles, PendingApprovalHandle[J]{ID: id, Job: entry.job})
		}
		delete(r.entries, id)
	}
	r.mu.Unlock()
	r.ownerWG.Wait()
	r.callbackWG.Wait()
	return handles
}

func (r *PendingApprovalRegistry[J]) expire(id string, expected *pendingApprovalEntry[J]) {
	if r == nil || expected == nil {
		return
	}
	r.mu.Lock()
	entry, ok := r.entries[id]
	if !ok || entry != expected {
		r.mu.Unlock()
		return
	}
	delete(r.entries, id)
	var claim PendingApprovalClaim[J]
	if r.onExpire != nil {
		if r.claims == nil {
			r.claims = make(map[string]*pendingApprovalClaimOwner[J])
		}
		owner := &pendingApprovalClaimOwner[J]{registry: r, id: id, entry: entry}
		r.claims[id] = owner
		r.ownerWG.Add(1)
		r.callbackWG.Add(1)
		claim = PendingApprovalClaim[J]{ID: id, Job: entry.job, owner: owner}
	}
	r.mu.Unlock()
	if r.onExpire != nil {
		defer r.callbackWG.Done()
		defer r.CompleteClaim(claim)
		r.onExpire(claim)
	}
}

// ResolveApprovalCommit distinguishes a failed pre-commit resolve from a
// post-commit audit failure. Callers that claimed an in-memory approval handle
// can restore it only when the durable record is still pending.
func ResolveApprovalCommit(ctx context.Context, g *guard.Guard, approvalID string, status guard.ApprovalStatus, actor, comment string) (ApprovalCommitState, guard.ApprovalRecord, error) {
	approvalID = strings.TrimSpace(approvalID)
	if g == nil {
		return ApprovalCommitUnknown, guard.ApprovalRecord{}, fmt.Errorf("approvals are unavailable")
	}
	if approvalID == "" {
		return ApprovalCommitUnknown, guard.ApprovalRecord{}, fmt.Errorf("approval id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	resolveErr := g.ResolveApproval(ctx, approvalID, status, strings.TrimSpace(actor), strings.TrimSpace(comment))
	if resolveErr == nil {
		return ApprovalCommitCommitted, guard.ApprovalRecord{ID: approvalID, Status: status}, nil
	}
	rec, ok, getErr := g.GetApproval(ctx, approvalID)
	if getErr != nil {
		return ApprovalCommitUnknown, guard.ApprovalRecord{}, errors.Join(ErrApprovalCommitIndeterminate, resolveErr, getErr)
	}
	if !ok {
		return ApprovalCommitUnknown, guard.ApprovalRecord{}, errors.Join(resolveErr, guard.ErrApprovalNotFound)
	}
	switch rec.Status {
	case status:
		return ApprovalCommitCommitted, rec, resolveErr
	case guard.ApprovalPending:
		return ApprovalCommitPending, rec, resolveErr
	default:
		return ApprovalCommitUnknown, rec, errors.Join(resolveErr, fmt.Errorf("approval status is %q, want %q", rec.Status, status))
	}
}

// ExpirePendingApproval resolves the approval first, then terminates its task.
// Resolve is a compare-and-set from pending, so an approve/deny winner cannot be
// overwritten by a late timer.
func ExpirePendingApproval(ctx context.Context, g *guard.Guard, store daemonruntime.TaskUpdater, approvalID, taskID, actor string) error {
	approvalID = strings.TrimSpace(approvalID)
	taskID = strings.TrimSpace(taskID)
	state, _, resolveErr := ResolveApprovalCommit(ctx, g, approvalID, guard.ApprovalExpired, actor, "approval expired")
	switch state {
	case ApprovalCommitPending:
		applied, updateErr := FailPendingApprovalTask(store, taskID, approvalID, ApprovalExpiryResolutionFailedTaskError)
		if !applied && updateErr == nil {
			return errors.Join(resolveErr, ErrApprovalTaskStateUnchanged)
		}
		if updateErr == nil {
			return resolveErr
		}
		return errors.Join(ErrApprovalTaskFinalizationFailed, resolveErr, updateErr)
	case ApprovalCommitCommitted:
		// Continue and persist the task terminal state below.
	default:
		return resolveErr
	}
	if store == nil || taskID == "" {
		return errors.Join(resolveErr, ErrApprovalTaskStateUnchanged)
	}
	applied := false
	finishedAt := time.Now().UTC()
	updateErr := store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		if info == nil || info.Status != daemonruntime.TaskPending || strings.TrimSpace(info.ApprovalRequestID) != approvalID {
			return
		}
		applied = true
		info.Status = daemonruntime.TaskCanceled
		info.Error = ApprovalExpiredTaskError
		info.FinishedAt = &finishedAt
		ClearTaskPendingApprovalFields(info)
		info.ApprovalRequestID = approvalID
	})
	if updateErr == nil {
		if !applied {
			return errors.Join(resolveErr, ErrApprovalTaskStateUnchanged)
		}
		return resolveErr
	}

	failed, failErr := FailPendingApprovalTask(store, taskID, approvalID, ApprovalExpiryResolutionFailedTaskError)
	if !applied && !failed && failErr == nil {
		return errors.Join(resolveErr, updateErr, ErrApprovalTaskStateUnchanged)
	}
	if failErr != nil {
		return errors.Join(ErrApprovalTaskFinalizationFailed, resolveErr, updateErr, failErr)
	}
	return errors.Join(resolveErr, updateErr, failErr)
}

// FailPendingApprovalTask conditionally terminates only the pending task that
// still owns approvalID. It leaves a concurrent decision winner untouched.
func FailPendingApprovalTask(store daemonruntime.TaskUpdater, taskID, approvalID, taskError string) (bool, error) {
	if store == nil || strings.TrimSpace(taskID) == "" || strings.TrimSpace(approvalID) == "" {
		return false, nil
	}
	taskError = strings.TrimSpace(taskError)
	if taskError == "" {
		taskError = ApprovalExpiryResolutionFailedTaskError
	}
	applied := false
	finishedAt := time.Now().UTC()
	err := store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		if info == nil || info.Status != daemonruntime.TaskPending || strings.TrimSpace(info.ApprovalRequestID) != approvalID {
			return
		}
		applied = true
		info.Status = daemonruntime.TaskFailed
		info.Error = taskError
		info.FinishedAt = &finishedAt
		ClearTaskPendingApprovalFields(info)
	})
	return applied, err
}
