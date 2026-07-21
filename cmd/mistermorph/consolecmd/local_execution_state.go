package consolecmd

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
)

var errConsoleExecutionClosed = errors.New("console execution state is closed")

// consoleExecutionState owns the resources used to admit, queue, resume, and
// stop Console task executions. Its mutex makes ownership transfer atomic with
// Close, so a generation reference always belongs to either the state or the
// caller, never neither.
type consoleExecutionState struct {
	mu sync.Mutex

	runner           *runtimecore.ConversationRunner[string, consoleLocalTaskJob]
	pendingJobs      map[string]consoleLocalTaskJob
	pendingApprovals *runtimecore.PendingApprovalRegistry[consoleLocalTaskJob]
	runControl       *runtimecontrol.RunControl
	workersCtx       context.Context
	cancelWorkers    context.CancelFunc
	onDrop           func(string, consoleLocalTaskJob)
	onApprovalClose  func(string, consoleLocalTaskJob)
	closed           bool
}

func newConsoleExecutionState(
	onApprovalExpire func(runtimecore.PendingApprovalClaim[consoleLocalTaskJob]),
	onApprovalClose func(string, consoleLocalTaskJob),
) *consoleExecutionState {
	workersCtx, cancelWorkers := context.WithCancel(context.Background())
	return &consoleExecutionState{
		pendingJobs:      make(map[string]consoleLocalTaskJob),
		pendingApprovals: runtimecore.NewPendingApprovalRegistry(onApprovalExpire),
		runControl:       runtimecontrol.New(),
		workersCtx:       workersCtx,
		cancelWorkers:    cancelWorkers,
		onApprovalClose:  onApprovalClose,
	}
}

func (s *consoleExecutionState) addPendingJob(job consoleLocalTaskJob) error {
	if s == nil {
		return errConsoleExecutionClosed
	}
	taskID := strings.TrimSpace(job.TaskID)
	if taskID == "" {
		return errors.New("task id is required")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errConsoleExecutionClosed
	}
	previous, replaced := s.pendingJobs[taskID]
	s.pendingJobs[taskID] = job
	s.mu.Unlock()

	if replaced && previous.Generation != nil {
		previous.Generation.release()
	}
	return nil
}

func (s *consoleExecutionState) takePendingJob(taskID string) (consoleLocalTaskJob, bool) {
	var zero consoleLocalTaskJob
	if s == nil {
		return zero, false
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return zero, false
	}

	s.mu.Lock()
	job, ok := s.pendingJobs[taskID]
	if ok {
		delete(s.pendingJobs, taskID)
	}
	s.mu.Unlock()
	return job, ok
}

func (s *consoleExecutionState) addPendingApproval(approvalID string, job consoleLocalTaskJob, expiresAt time.Time) error {
	if s == nil {
		return errConsoleExecutionClosed
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return errors.New("approval id is required")
	}

	s.mu.Lock()
	if s.closed || s.pendingApprovals == nil {
		s.mu.Unlock()
		return errConsoleExecutionClosed
	}
	previous, replaced, err := s.pendingApprovals.Register(approvalID, job, expiresAt)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if replaced && previous.Generation != nil {
		previous.Generation.release()
	}
	return nil
}

func (s *consoleExecutionState) claimPendingApproval(approvalID string) (runtimecore.PendingApprovalClaim[consoleLocalTaskJob], runtimecore.PendingApprovalClaimState, error) {
	var zero runtimecore.PendingApprovalClaim[consoleLocalTaskJob]
	if s == nil {
		return zero, runtimecore.PendingApprovalClaimMissing, runtimecore.ErrPendingApprovalRegistryUnavailable
	}
	s.mu.Lock()
	if s.closed || s.pendingApprovals == nil {
		s.mu.Unlock()
		return zero, runtimecore.PendingApprovalClaimMissing, runtimecore.ErrPendingApprovalRegistryUnavailable
	}
	registry := s.pendingApprovals
	s.mu.Unlock()
	return registry.Claim(approvalID)
}

func (s *consoleExecutionState) completePendingApprovalClaim(claim runtimecore.PendingApprovalClaim[consoleLocalTaskJob]) {
	if s == nil {
		return
	}
	s.mu.Lock()
	registry := s.pendingApprovals
	s.mu.Unlock()
	if registry != nil {
		registry.CompleteClaim(claim)
	}
}

func (s *consoleExecutionState) restorePendingApprovalClaim(claim runtimecore.PendingApprovalClaim[consoleLocalTaskJob], retryAt time.Time) error {
	if s == nil {
		return runtimecore.ErrPendingApprovalRegistryUnavailable
	}
	s.mu.Lock()
	if s.closed || s.pendingApprovals == nil {
		s.mu.Unlock()
		return runtimecore.ErrPendingApprovalRegistryUnavailable
	}
	registry := s.pendingApprovals
	s.mu.Unlock()
	return registry.RestoreClaim(claim, retryAt)
}

func (s *consoleExecutionState) pendingApproval(approvalID string) (consoleLocalTaskJob, bool) {
	var zero consoleLocalTaskJob
	if s == nil {
		return zero, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingApprovals == nil {
		return zero, false
	}
	return s.pendingApprovals.Get(approvalID)
}

func (s *consoleExecutionState) close() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.closed = true
	cancelWorkers := s.cancelWorkers
	s.cancelWorkers = nil
	runner := s.runner
	pendingJobs := s.pendingJobs
	s.pendingJobs = nil
	pendingApprovals := s.pendingApprovals
	s.mu.Unlock()

	if cancelWorkers != nil {
		cancelWorkers()
	}
	if runner != nil {
		runner.WaitClosed()
	}
	for taskID, job := range pendingJobs {
		if s.onDrop != nil {
			s.onDrop(job.ConversationKey, job)
		} else if job.Generation != nil {
			job.Generation.release()
		}
		delete(pendingJobs, taskID)
	}
	if pendingApprovals != nil {
		for _, handle := range pendingApprovals.Close() {
			job := handle.Job
			if s.onApprovalClose != nil {
				s.onApprovalClose(handle.ID, job)
			} else if s.onDrop != nil {
				s.onDrop(job.ConversationKey, job)
			} else if job.Generation != nil {
				job.Generation.release()
			}
		}
	}
	return true
}
