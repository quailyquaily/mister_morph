package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

const defaultCompletedTTL = 30 * time.Minute

type queuedTask struct {
	info   *TaskInfo
	ctx    context.Context
	cancel context.CancelFunc

	// resumeApprovalID is set when re-queued to resume a paused run from an approval request.
	resumeApprovalID string
}

type TaskStore struct {
	mu           sync.RWMutex
	tasks        map[string]*queuedTask
	queue        chan *queuedTask
	done         chan struct{} // closed by Close() to signal shutdown
	closeOnce    sync.Once
	completedTTL time.Duration
}

func NewTaskStore(maxQueue int) *TaskStore {
	if maxQueue <= 0 {
		maxQueue = 100
	}
	s := &TaskStore{
		tasks:        make(map[string]*queuedTask),
		queue:        make(chan *queuedTask, maxQueue),
		done:         make(chan struct{}),
		completedTTL: defaultCompletedTTL,
	}
	go s.evictLoop()
	return s
}

func (s *TaskStore) Enqueue(parent context.Context, task string, model string, timeout time.Duration) (*TaskInfo, error) {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	select {
	case <-s.done:
		return nil, fmt.Errorf("store is closed")
	default:
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

// Next blocks until a task is available or the store is closed.
// Returns (nil, false) when the store is closed.
func (s *TaskStore) Next() (*queuedTask, bool) {
	select {
	case qt, ok := <-s.queue:
		return qt, ok
	case <-s.done:
		return nil, false
	}
}

// Close signals the store to shut down. It cancels all in-flight task
// contexts and closes the queue channel so the worker exits cleanly.
func (s *TaskStore) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.cancelAll()
	})
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

// cancelAll cancels every in-flight task context. Called during shutdown.
func (s *TaskStore) cancelAll() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, qt := range s.tasks {
		if qt != nil && qt.cancel != nil {
			qt.cancel()
		}
	}
}

// isTerminal returns true for task statuses that represent a finished task.
func isTerminal(st TaskStatus) bool {
	return st == TaskDone || st == TaskFailed || st == TaskCanceled
}

// evictLoop periodically removes completed/failed/canceled tasks that have
// been finished for longer than the configured TTL. This prevents the tasks
// map from growing unbounded in a long-running daemon.
func (s *TaskStore) evictLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.evictExpired()
		}
	}
}

func (s *TaskStore) evictExpired() {
	now := time.Now()
	ttl := s.completedTTL
	if ttl <= 0 {
		ttl = defaultCompletedTTL
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, qt := range s.tasks {
		if qt == nil || qt.info == nil {
			delete(s.tasks, id)
			continue
		}
		if !isTerminal(qt.info.Status) {
			continue
		}
		if qt.info.FinishedAt != nil && now.Sub(*qt.info.FinishedAt) > ttl {
			delete(s.tasks, id)
		}
	}
}
