package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

type queuedTask struct {
	info   *TaskInfo
	ctx    context.Context
	cancel context.CancelFunc

	// resumeApprovalID is set when re-queued to resume a paused run from an approval request.
	resumeApprovalID string
}

type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*queuedTask
	queue chan *queuedTask
}

func NewTaskStore(maxQueue int) *TaskStore {
	if maxQueue <= 0 {
		maxQueue = 100
	}
	return &TaskStore{
		tasks: make(map[string]*queuedTask),
		queue: make(chan *queuedTask, maxQueue),
	}
}

func (s *TaskStore) Enqueue(parent context.Context, task string, model string, timeout time.Duration) (*TaskInfo, error) {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	id := fmt.Sprintf("%x", rand.Uint64())
	now := time.Now()
	ctx, cancel := context.WithTimeout(parent, timeout)

	info := &TaskInfo{
		ID:        id,
		Status:    TaskQueued,
		Task:      task,
		Model:     model,
		Timeout:   timeout.String(),
		CreatedAt: now,
	}
	qt := &queuedTask{info: info, ctx: ctx, cancel: cancel}

	s.mu.Lock()
	s.tasks[id] = qt
	s.mu.Unlock()

	select {
	case s.queue <- qt:
		return info, nil
	default:
		qt.cancel()
		s.mu.Lock()
		delete(s.tasks, id)
		s.mu.Unlock()
		return nil, fmt.Errorf("queue is full")
	}
}

func (s *TaskStore) Get(id string) (*TaskInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	qt, ok := s.tasks[id]
	if !ok || qt == nil || qt.info == nil {
		return nil, false
	}
	// Return a shallow copy for safe reads.
	cp := *qt.info
	return &cp, true
}

func (s *TaskStore) Next() *queuedTask {
	return <-s.queue
}

func (s *TaskStore) Update(id string, fn func(info *TaskInfo)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	qt := s.tasks[id]
	if qt == nil || qt.info == nil {
		return
	}
	fn(qt.info)
}

func (s *TaskStore) EnqueueResumeByApprovalID(approvalRequestID string) (string, error) {
	approvalRequestID = strings.TrimSpace(approvalRequestID)
	if approvalRequestID == "" {
		return "", fmt.Errorf("missing approval_request_id")
	}

	s.mu.Lock()
	var qt *queuedTask
	for _, t := range s.tasks {
		if t == nil || t.info == nil {
			continue
		}
		if strings.TrimSpace(t.info.ApprovalRequestID) != approvalRequestID {
			continue
		}
		if t.info.Status != TaskPending {
			continue
		}
		qt = t
		break
	}
	if qt == nil {
		s.mu.Unlock()
		return "", fmt.Errorf("no pending task found for approval_request_id %q", approvalRequestID)
	}
	if strings.TrimSpace(qt.resumeApprovalID) != "" {
		s.mu.Unlock()
		return "", fmt.Errorf("task already queued for resume")
	}

	qt.resumeApprovalID = approvalRequestID
	select {
	case s.queue <- qt:
		s.mu.Unlock()
		return qt.info.ID, nil
	default:
		qt.resumeApprovalID = ""
		s.mu.Unlock()
		return "", fmt.Errorf("queue is full")
	}
}

func (s *TaskStore) FailPendingByApprovalID(approvalRequestID string, errMsg string) (string, bool) {
	approvalRequestID = strings.TrimSpace(approvalRequestID)
	if approvalRequestID == "" {
		return "", false
	}

	var cancel context.CancelFunc
	var id string
	now := time.Now()

	s.mu.Lock()
	for _, qt := range s.tasks {
		if qt == nil || qt.info == nil {
			continue
		}
		if strings.TrimSpace(qt.info.ApprovalRequestID) != approvalRequestID {
			continue
		}
		if qt.info.Status != TaskPending {
			continue
		}
		id = qt.info.ID
		qt.info.Status = TaskFailed
		qt.info.Error = strings.TrimSpace(errMsg)
		qt.info.FinishedAt = &now
		cancel = qt.cancel
		break
	}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return id, cancel != nil
}
