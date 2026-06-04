package runtimecontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
)

var ErrStoppedByUser = errors.New("stopped by user")

type ActiveRun struct {
	Runtime         string
	ConversationKey string
	TopicID         string
	TaskID          string
	RunID           string
	Cancel          context.CancelCauseFunc
	Snapshot        func() string
	SteerQueue      *SteerQueue
	EventSink       agent.EventSink
}

type StopResult struct {
	Found    bool   `json:"found"`
	Progress string `json:"progress,omitempty"`
}

type SteerResult struct {
	Found  bool `json:"found"`
	Queued bool `json:"queued"`
}

type RunControl struct {
	mu     sync.Mutex
	active map[runKey]*activeRun
}

type runKey struct {
	runtime         string
	conversationKey string
}

type activeRun struct {
	Runtime         string
	ConversationKey string
	TopicID         string
	TaskID          string
	RunID           string
	Cancel          context.CancelCauseFunc
	Snapshot        func() string
	SteerQueue      *SteerQueue
	EventSink       agent.EventSink

	stopRequested bool
	stopReason    string
}

type RunLease struct {
	Context    context.Context
	SteerQueue *SteerQueue

	control         *RunControl
	runtime         string
	conversationKey string
	taskID          string
	stopCancel      context.CancelCauseFunc
	timeoutCancel   context.CancelFunc
	finished        bool
}

func New() *RunControl {
	return &RunControl{
		active: map[runKey]*activeRun{},
	}
}

func (c *RunControl) StartLease(parent context.Context, timeout time.Duration, run ActiveRun) (*RunLease, error) {
	if parent == nil {
		parent = context.Background()
	}
	stopCtx, stopCancel := context.WithCancelCause(parent)
	runCtx := stopCtx
	timeoutCancel := context.CancelFunc(func() {})
	if timeout > 0 {
		runCtx, timeoutCancel = context.WithTimeout(stopCtx, timeout)
	}
	queue := run.SteerQueue
	if queue == nil {
		queue = NewSteerQueue(0)
	}
	run.Cancel = stopCancel
	run.SteerQueue = queue
	if err := c.Start(run); err != nil {
		timeoutCancel()
		stopCancel(nil)
		return nil, err
	}
	return &RunLease{
		Context:         runCtx,
		SteerQueue:      queue,
		control:         c,
		runtime:         strings.TrimSpace(run.Runtime),
		conversationKey: strings.TrimSpace(run.ConversationKey),
		taskID:          strings.TrimSpace(run.TaskID),
		stopCancel:      stopCancel,
		timeoutCancel:   timeoutCancel,
	}, nil
}

func (l *RunLease) UserStopped() bool {
	return l != nil && errors.Is(context.Cause(l.Context), ErrStoppedByUser)
}

func (l *RunLease) Finish() bool {
	if l == nil || l.finished {
		return false
	}
	l.finished = true
	if l.timeoutCancel != nil {
		l.timeoutCancel()
	}
	if l.stopCancel != nil {
		l.stopCancel(nil)
	}
	if l.control == nil {
		return false
	}
	return l.control.Finish(l.runtime, l.conversationKey, l.taskID)
}

func (c *RunControl) Start(run ActiveRun) error {
	if c == nil {
		return fmt.Errorf("run control is nil")
	}
	key, err := keyFrom(run.Runtime, run.ConversationKey)
	if err != nil {
		return err
	}
	taskID := strings.TrimSpace(run.TaskID)
	if taskID == "" {
		return fmt.Errorf("task id is required")
	}
	entry := &activeRun{
		Runtime:         key.runtime,
		ConversationKey: key.conversationKey,
		TopicID:         strings.TrimSpace(run.TopicID),
		TaskID:          taskID,
		RunID:           strings.TrimSpace(run.RunID),
		Cancel:          run.Cancel,
		Snapshot:        run.Snapshot,
		SteerQueue:      run.SteerQueue,
		EventSink:       run.EventSink,
	}
	if entry.SteerQueue == nil {
		entry.SteerQueue = NewSteerQueue(0)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.active[key]; exists {
		return fmt.Errorf("active run already exists for runtime %q conversation %q", key.runtime, key.conversationKey)
	}
	c.active[key] = entry
	return nil
}

func (c *RunControl) Stop(runtime string, conversationKey string, reason string) StopResult {
	key, err := keyFrom(runtime, conversationKey)
	if c == nil || err != nil {
		return StopResult{}
	}
	return c.stopByKey(key, reason, "")
}

func (c *RunControl) StopTask(runtime string, taskID string, reason string) StopResult {
	runtime = strings.TrimSpace(runtime)
	taskID = strings.TrimSpace(taskID)
	if c == nil || runtime == "" || taskID == "" {
		return StopResult{}
	}

	c.mu.Lock()
	var key runKey
	found := false
	for candidate, entry := range c.active {
		if candidate.runtime == runtime && entry != nil && strings.TrimSpace(entry.TaskID) == taskID {
			key = candidate
			found = true
			break
		}
	}
	c.mu.Unlock()
	if !found {
		return StopResult{}
	}
	return c.stopByKey(key, reason, taskID)
}

func (c *RunControl) Finish(runtime string, conversationKey string, taskID string) bool {
	key, err := keyFrom(runtime, conversationKey)
	if c == nil || err != nil {
		return false
	}
	taskID = strings.TrimSpace(taskID)
	c.mu.Lock()
	entry := c.active[key]
	if entry == nil || strings.TrimSpace(entry.TaskID) != taskID {
		c.mu.Unlock()
		return false
	}
	wasStopped := entry.stopRequested
	if entry.SteerQueue != nil {
		entry.SteerQueue.Close()
	}
	delete(c.active, key)
	c.mu.Unlock()

	if wasStopped {
		emitControlEvent(entry, agent.EventKindRunStopped, entry.stopReason, "")
	}
	return true
}

func (c *RunControl) Steer(runtime string, conversationKey string, input string) SteerResult {
	key, err := keyFrom(runtime, conversationKey)
	input = strings.TrimSpace(input)
	if c == nil || err != nil || input == "" {
		return SteerResult{}
	}

	c.mu.Lock()
	entry := c.active[key]
	if entry == nil || entry.SteerQueue == nil {
		c.mu.Unlock()
		return SteerResult{}
	}
	if _, pushErr := entry.SteerQueue.Push(input); pushErr != nil {
		c.mu.Unlock()
		return SteerResult{Found: true}
	}
	c.mu.Unlock()
	emitControlEvent(entry, agent.EventKindSteerQueued, "", input)
	return SteerResult{Found: true, Queued: true}
}

func (c *RunControl) stopByKey(key runKey, reason string, expectedTaskID string) StopResult {
	reason = strings.TrimSpace(reason)
	expectedTaskID = strings.TrimSpace(expectedTaskID)
	c.mu.Lock()
	entry := c.active[key]
	if entry == nil || (expectedTaskID != "" && strings.TrimSpace(entry.TaskID) != expectedTaskID) {
		c.mu.Unlock()
		return StopResult{}
	}
	shouldCancel := !entry.stopRequested
	if shouldCancel {
		entry.stopRequested = true
		entry.stopReason = reason
	}
	c.mu.Unlock()

	if shouldCancel {
		emitControlEvent(entry, agent.EventKindRunStopRequested, reason, "")
		if entry.Cancel != nil {
			entry.Cancel(ErrStoppedByUser)
		}
	}
	return StopResult{
		Found:    true,
		Progress: snapshotFor(entry),
	}
}

func keyFrom(runtime string, conversationKey string) (runKey, error) {
	key := runKey{
		runtime:         strings.TrimSpace(runtime),
		conversationKey: strings.TrimSpace(conversationKey),
	}
	if key.runtime == "" {
		return runKey{}, fmt.Errorf("runtime is required")
	}
	if key.conversationKey == "" {
		return runKey{}, fmt.Errorf("conversation key is required")
	}
	return key, nil
}

func snapshotFor(entry *activeRun) string {
	if entry == nil || entry.Snapshot == nil {
		return ""
	}
	return strings.TrimSpace(entry.Snapshot())
}

func emitControlEvent(entry *activeRun, kind string, reason string, text string) {
	if entry == nil || entry.EventSink == nil {
		return
	}
	agent.EmitEvent(context.Background(), entry.EventSink, agent.Event{
		Kind:            kind,
		RunID:           entry.RunID,
		TaskID:          entry.TaskID,
		ConversationKey: entry.ConversationKey,
		TopicID:         entry.TopicID,
		Reason:          strings.TrimSpace(reason),
		Text:            strings.TrimSpace(text),
	})
}

func StopFeedback(found bool, progress string) string {
	if !found {
		return "当前没有正在运行的任务。"
	}
	progress = strings.TrimSpace(progress)
	if progress == "" {
		return "已请求停止当前任务。"
	}
	return "已请求停止当前任务。\n当前进展：" + progress
}

func SteerFeedback(found bool, queued bool) string {
	if !found {
		return "当前没有正在运行的任务。"
	}
	if !queued {
		return "当前任务正在运行，但暂时无法接收新的补充输入。"
	}
	return "已收到，会作为当前任务的补充输入处理。"
}
