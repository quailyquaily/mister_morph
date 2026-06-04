package runtimecontrol

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
)

type recordingSink struct {
	mu     sync.Mutex
	events []agent.Event
}

func (s *recordingSink) HandleEvent(_ context.Context, event agent.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *recordingSink) all() []agent.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]agent.Event, len(s.events))
	copy(out, s.events)
	return out
}

func TestRunControlRejectsDuplicateActiveRun(t *testing.T) {
	control := New()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	err := control.Start(ActiveRun{
		Runtime:         "console",
		ConversationKey: "topic:1",
		TaskID:          "task_1",
		RunID:           "run_1",
		Cancel:          cancel,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	err = control.Start(ActiveRun{
		Runtime:         "console",
		ConversationKey: "topic:1",
		TaskID:          "task_2",
		RunID:           "run_2",
		Cancel:          cancel,
	})
	if err == nil {
		t.Fatal("Start() duplicate error = nil")
	}
	if ctx.Err() != nil {
		t.Fatalf("duplicate Start() canceled ctx: %v", ctx.Err())
	}
}

func TestRunControlStopIsIdempotentAndEmitsEvents(t *testing.T) {
	control := New()
	sink := &recordingSink{}
	ctx, cancel := context.WithCancelCause(context.Background())

	err := control.Start(ActiveRun{
		Runtime:         "console",
		ConversationKey: "topic:1",
		TopicID:         "topic-1",
		TaskID:          "task_1",
		RunID:           "run_1",
		Cancel:          cancel,
		EventSink:       sink,
		Snapshot: func() string {
			return "LLM 轮次 2，计划 1/3"
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	first := control.Stop("console", "topic:1", "user_stop")
	if !first.Found {
		t.Fatalf("first Stop() = %#v, want found", first)
	}
	if first.Progress != "LLM 轮次 2，计划 1/3" {
		t.Fatalf("first progress = %q, want snapshot text", first.Progress)
	}
	if !errors.Is(context.Cause(ctx), ErrStoppedByUser) {
		t.Fatalf("context cause = %v, want ErrStoppedByUser", context.Cause(ctx))
	}

	second := control.Stop("console", "topic:1", "again")
	if !second.Found {
		t.Fatalf("second Stop() = %#v, want same active run", second)
	}
	if !errors.Is(context.Cause(ctx), ErrStoppedByUser) {
		t.Fatalf("second Stop() cause = %v, want original ErrStoppedByUser", context.Cause(ctx))
	}

	if !control.Finish("console", "topic:1", "task_1") {
		t.Fatal("Finish() = false, want true")
	}
	missing := control.Stop("console", "topic:1", "after_finish")
	if missing.Found {
		t.Fatalf("Stop() after Finish() = %#v, want not found", missing)
	}

	events := sink.all()
	if len(events) != 2 {
		t.Fatalf("events = %#v, want stop requested and stopped", events)
	}
	if events[0].Kind != agent.EventKindRunStopRequested || events[1].Kind != agent.EventKindRunStopped {
		t.Fatalf("event kinds = %q, %q", events[0].Kind, events[1].Kind)
	}
	if events[0].TaskID != "task_1" || events[0].ConversationKey != "topic:1" || events[0].Reason != "user_stop" {
		t.Fatalf("stop event = %#v, want task/conversation/reason", events[0])
	}
}

func TestRunControlStopTaskFindsActiveRunByTaskID(t *testing.T) {
	control := New()
	ctx, cancel := context.WithCancelCause(context.Background())

	if err := control.Start(ActiveRun{
		Runtime:         "console",
		ConversationKey: "topic:1",
		TaskID:          "task_1",
		Cancel:          cancel,
		Snapshot: func() string {
			return "计划 2/4"
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	stop := control.StopTask("console", "task_1", "/stop")
	if !stop.Found {
		t.Fatalf("StopTask() = %#v, want found", stop)
	}
	if stop.Progress != "计划 2/4" {
		t.Fatalf("StopTask().Progress = %q, want snapshot text", stop.Progress)
	}
	if !errors.Is(context.Cause(ctx), ErrStoppedByUser) {
		t.Fatalf("context cause = %v, want ErrStoppedByUser", context.Cause(ctx))
	}

	missing := control.StopTask("console", "task_missing", "/stop")
	if missing.Found {
		t.Fatalf("StopTask(missing) = %#v, want not found", missing)
	}
}

func TestRunControlSteerQueuesAndDrainsInOrder(t *testing.T) {
	control := New()
	sink := &recordingSink{}
	_, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	queue := NewSteerQueue(0)
	if err := control.Start(ActiveRun{
		Runtime:         "console",
		ConversationKey: "topic:1",
		TaskID:          "task_1",
		RunID:           "run_1",
		Cancel:          cancel,
		SteerQueue:      queue,
		EventSink:       sink,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	first := control.Steer("console", "topic:1", "prefer short answer")
	second := control.Steer("console", "topic:1", "also mention risks")
	if !first.Found || !first.Queued {
		t.Fatalf("first Steer() = %#v, want queued", first)
	}
	if !second.Found || !second.Queued {
		t.Fatalf("second Steer() = %#v, want queued", second)
	}

	items := queue.Drain()
	if len(items) != 2 {
		t.Fatalf("Drain() len = %d, want 2", len(items))
	}
	if items[0] != "prefer short answer" || items[1] != "also mention risks" {
		t.Fatalf("Drain() = %#v, want input order", items)
	}
	if again := queue.Drain(); len(again) != 0 {
		t.Fatalf("second Drain() = %#v, want empty", again)
	}
	missing := control.Steer("console", "topic:missing", "ignored")
	if missing.Found || missing.Queued {
		t.Fatalf("Steer() missing = %#v, want not found", missing)
	}

	events := sink.all()
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	for i, ev := range events {
		if ev.Kind != agent.EventKindSteerQueued {
			t.Fatalf("event %d kind = %q, want steer_queued", i, ev.Kind)
		}
	}
}

func TestRunControlFinishClosesSteerQueue(t *testing.T) {
	control := New()
	_, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	queue := NewSteerQueue(0)
	if err := control.Start(ActiveRun{
		Runtime:         "console",
		ConversationKey: "topic:1",
		TaskID:          "task_1",
		Cancel:          cancel,
		SteerQueue:      queue,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !control.Finish("console", "topic:1", "task_1") {
		t.Fatal("Finish() = false, want true")
	}
	if _, err := queue.Push("late input"); err == nil {
		t.Fatal("Push() after Finish() error = nil")
	}
	if got := queue.Drain(); len(got) != 0 {
		t.Fatalf("Drain() after Finish() = %#v, want empty", got)
	}
}

func TestRunControlSteerReportsClosedQueue(t *testing.T) {
	control := New()
	_, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	queue := NewSteerQueue(0)
	if err := control.Start(ActiveRun{
		Runtime:         "console",
		ConversationKey: "topic:1",
		TaskID:          "task_1",
		Cancel:          cancel,
		SteerQueue:      queue,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	queue.Close()

	got := control.Steer("console", "topic:1", "late input")
	if !got.Found || got.Queued {
		t.Fatalf("Steer() = %#v, want found but not queued", got)
	}
}

func TestSteerQueueDrainAndCloseRejectsLaterInput(t *testing.T) {
	queue := NewSteerQueue(0)
	if _, err := queue.Push("final note"); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	items := queue.DrainAndClose()
	if len(items) != 1 || items[0] != "final note" {
		t.Fatalf("DrainAndClose() = %#v, want queued item", items)
	}
	if _, err := queue.Push("too late"); err == nil {
		t.Fatal("Push() after DrainAndClose() error = nil")
	}
	if got := queue.Drain(); len(got) != 0 {
		t.Fatalf("Drain() after DrainAndClose() = %#v, want empty", got)
	}
}

func TestRunControlStartLeaseRegistersRunAndCleansUp(t *testing.T) {
	control := New()
	sink := &recordingSink{}

	lease, err := control.StartLease(context.Background(), time.Minute, ActiveRun{
		Runtime:         "telegram",
		ConversationKey: "chat:1",
		TopicID:         "topic_1",
		TaskID:          "task_1",
		RunID:           "run_1",
		EventSink:       sink,
		Snapshot: func() string {
			return "计划 1/2"
		},
	})
	if err != nil {
		t.Fatalf("StartLease() error = %v", err)
	}
	if lease == nil || lease.Context == nil || lease.SteerQueue == nil {
		t.Fatalf("lease = %#v, want context and steer queue", lease)
	}

	steer := control.Steer("telegram", "chat:1", "prefer concise")
	if !steer.Found || !steer.Queued {
		t.Fatalf("Steer() = %#v, want queued", steer)
	}
	if items := lease.SteerQueue.Drain(); len(items) != 1 || items[0] != "prefer concise" {
		t.Fatalf("lease steer queue = %#v, want queued input", items)
	}

	stop := control.Stop("telegram", "chat:1", "/stop")
	if !stop.Found {
		t.Fatalf("Stop() = %#v, want found", stop)
	}
	if stop.Progress != "计划 1/2" {
		t.Fatalf("Stop().Progress = %q, want snapshot text", stop.Progress)
	}
	if !lease.UserStopped() {
		t.Fatalf("UserStopped() = false, cause=%v", context.Cause(lease.Context))
	}
	if !lease.Finish() {
		t.Fatal("Finish() = false, want true")
	}
	if again := control.Stop("telegram", "chat:1", "again"); again.Found {
		t.Fatalf("Stop() after Finish() = %#v, want not found", again)
	}
	if lease.Finish() {
		t.Fatal("second Finish() = true, want false")
	}

	events := sink.all()
	stopRequested := agent.Event{}
	for _, event := range events {
		if event.Kind == agent.EventKindRunStopRequested {
			stopRequested = event
			break
		}
	}
	if stopRequested.Kind == "" {
		t.Fatalf("events = %#v, want stop requested", events)
	}
	if stopRequested.TaskID != "task_1" || stopRequested.ConversationKey != "chat:1" || stopRequested.TopicID != "topic_1" {
		t.Fatalf("event metadata = %#v, want lease metadata", stopRequested)
	}
}

func TestFeedbackHelpersUsePlainState(t *testing.T) {
	if got := StopFeedback(false, ""); got != "当前没有正在运行的任务。" {
		t.Fatalf("StopFeedback(false, empty) = %q", got)
	}
	if got := StopFeedback(true, ""); got != "已请求停止当前任务。" {
		t.Fatalf("StopFeedback(true, empty) = %q", got)
	}
	if got := StopFeedback(true, "计划 1/3"); got != "已请求停止当前任务。\n当前进展：计划 1/3" {
		t.Fatalf("StopFeedback(true, progress) = %q", got)
	}
	if got := SteerFeedback(false, false); got != "当前没有正在运行的任务。" {
		t.Fatalf("SteerFeedback(false, false) = %q", got)
	}
	if got := SteerFeedback(true, false); got != "当前任务正在运行，但暂时无法接收新的补充输入。" {
		t.Fatalf("SteerFeedback(true, false) = %q", got)
	}
	if got := SteerFeedback(true, true); got != "👌" {
		t.Fatalf("SteerFeedback(true, true) = %q", got)
	}
}
