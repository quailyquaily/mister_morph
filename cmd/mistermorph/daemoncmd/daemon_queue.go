package daemoncmd

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
)

type queuedTask struct {
	taskID string
	task   string
	model  string
	ctx    context.Context
	cancel context.CancelFunc

	// resumeApprovalID is set when re-queued to resume a paused run from an approval request.
	resumeApprovalID string
	approvalID       string
	trigger          daemonruntime.TaskTrigger

	// Internal-only heartbeat fields.
	meta           map[string]any
	isHeartbeat    bool
	heartbeatState *heartbeatutil.State
}

type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*queuedTask
	queue chan *queuedTask
	view  daemonruntime.TaskView
}

func NewTaskStore(maxQueue int, view daemonruntime.TaskView) *TaskStore {
	if maxQueue <= 0 {
		maxQueue = 100
	}
	if view == nil {
		view = daemonruntime.NewMemoryStore(maxQueue)
	}
	return &TaskStore{
		tasks: make(map[string]*queuedTask),
		queue: make(chan *queuedTask, maxQueue),
		view:  view,
	}
}

func (s *TaskStore) Enqueue(parent context.Context, task string, model string, timeout time.Duration) (*daemonruntime.TaskInfo, error) {
	return s.EnqueueWithTrigger(parent, task, model, timeout, daemonruntime.TaskTrigger{})
}

func (s *TaskStore) EnqueueWithTrigger(parent context.Context, task string, model string, timeout time.Duration, trigger daemonruntime.TaskTrigger) (*daemonruntime.TaskInfo, error) {
	return s.enqueue(parent, task, model, timeout, nil, false, nil, trigger)
}

func (s *TaskStore) EnqueueHeartbeat(parent context.Context, task string, model string, timeout time.Duration, meta map[string]any, hbState *heartbeatutil.State) (*daemonruntime.TaskInfo, error) {
	return s.enqueue(parent, task, model, timeout, meta, true, hbState, daemonruntime.TaskTrigger{
		Source: "heartbeat",
		Event:  "heartbeat_tick",
		Ref:    "daemon/serve",
	})
}

func (s *TaskStore) enqueue(parent context.Context, task string, model string, timeout time.Duration, meta map[string]any, isHeartbeat bool, hbState *heartbeatutil.State, trigger daemonruntime.TaskTrigger) (*daemonruntime.TaskInfo, error) {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	if model == "" {
		model = "gpt-5.2"
	}

	id := fmt.Sprintf("%x", rand.Uint64())
	now := time.Now()
	ctx, cancel := context.WithTimeout(parent, timeout)

	info := &daemonruntime.TaskInfo{
		ID:        id,
		Status:    daemonruntime.TaskQueued,
		Task:      task,
		Model:     model,
		Timeout:   timeout.String(),
		CreatedAt: now,
	}
	qt := &queuedTask{
		taskID:  id,
		task:    task,
		model:   model,
		ctx:     ctx,
		cancel:  cancel,
		trigger: trigger,
	}
	qt.meta = meta
	qt.isHeartbeat = isHeartbeat
	qt.heartbeatState = hbState

	s.mu.Lock()
	s.tasks[id] = qt
	s.mu.Unlock()

	select {
	case s.queue <- qt:
		if s.view != nil {
			_ = daemonruntime.RecordTaskUpsert(s.view, *info, trigger)
		}
		return info, nil
	default:
		qt.cancel()
		s.mu.Lock()
		delete(s.tasks, id)
		s.mu.Unlock()
		return nil, fmt.Errorf("queue is full")
	}
}

func (s *TaskStore) Get(id string) (*daemonruntime.TaskInfo, bool) {
	if s.view == nil {
		return nil, false
	}
	return s.view.Get(id)
}

func (s *TaskStore) Next() *queuedTask {
	return <-s.queue
}

func (s *TaskStore) QueueLen() int {
	return len(s.queue)
}

func (s *TaskStore) List(opts daemonruntime.TaskListOptions) []daemonruntime.TaskInfo {
	if s.view != nil {
		return s.view.List(opts)
	}
	return nil
}

func (s *TaskStore) Update(id string, fn func(info *daemonruntime.TaskInfo)) {
	if fn == nil {
		return
	}
	if s.view != nil {
		_ = daemonruntime.RecordTaskUpdate(s.view, id, daemonruntime.TaskTrigger{}, fn)
	}
}

func (s *TaskStore) EnqueueResumeByApprovalID(approvalRequestID string) (string, error) {
	approvalRequestID = strings.TrimSpace(approvalRequestID)
	if approvalRequestID == "" {
		return "", fmt.Errorf("missing approval_request_id")
	}

	s.mu.Lock()
	var qt *queuedTask
	for _, t := range s.tasks {
		if t == nil {
			continue
		}
		if strings.TrimSpace(t.approvalID) != approvalRequestID {
			continue
		}
		info, ok := s.view.Get(t.taskID)
		if !ok || info == nil || info.Status != daemonruntime.TaskPending {
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
		return qt.taskID, nil
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
		if qt == nil {
			continue
		}
		if strings.TrimSpace(qt.approvalID) != approvalRequestID {
			continue
		}
		info, ok := s.view.Get(qt.taskID)
		if !ok || info == nil || info.Status != daemonruntime.TaskPending {
			continue
		}
		id = qt.taskID
		qt.approvalID = ""
		cancel = qt.cancel
		break
	}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cancel != nil && id != "" && s.view != nil {
		errText := strings.TrimSpace(errMsg)
		_ = daemonruntime.RecordTaskUpdate(s.view, id, daemonruntime.TaskTrigger{}, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskFailed
			info.Error = errText
			info.FinishedAt = &now
		})
	}
	return id, cancel != nil
}
