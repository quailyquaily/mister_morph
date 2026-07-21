package consolecmd

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
)

type stubConsoleSemanticObserver struct {
	summary string
}

func (s stubConsoleSemanticObserver) Summarize(_ context.Context, _ consoleObserveRequest) (string, error) {
	return s.summary, nil
}

func TestConsoleStreamHubEvictsDoneFrameWithoutSubscribers(t *testing.T) {
	hub := newConsoleStreamHub()
	taskID := "task-no-subscriber"

	hub.PublishSnapshot(taskID, "partial output")
	if _, ok := hub.Latest(taskID); !ok {
		t.Fatal("latest snapshot missing before completion")
	}

	hub.PublishFinal(taskID, "final output")
	if _, ok := hub.Latest(taskID); ok {
		t.Fatal("latest entry retained after done frame without subscribers")
	}
}

func TestConsoleStreamHubEvictsDoneFrameOnLastUnsubscribe(t *testing.T) {
	hub := newConsoleStreamHub()
	taskID := "task-with-subscriber"

	_, unsubscribe := hub.Subscribe(taskID)
	hub.PublishFinal(taskID, "final output")

	latest, ok := hub.Latest(taskID)
	if !ok {
		t.Fatal("latest entry missing while subscriber is connected")
	}
	if !latest.Done {
		t.Fatalf("latest.Done = %v, want true", latest.Done)
	}

	unsubscribe()
	if _, ok := hub.Latest(taskID); ok {
		t.Fatal("latest entry retained after last subscriber unsubscribed")
	}
}

func TestConsoleReplySinkDefersSnapshotsUntilGuardedFinal(t *testing.T) {
	hub := newConsoleStreamHub()
	taskID := "task-guarded-stream"
	_, unsubscribe := hub.Subscribe(taskID)
	defer unsubscribe()

	outputGuard := guard.New(guard.Config{Enabled: true}, nil, nil)
	sink := newConsoleReplySink(hub, taskID, nil, outputGuard)
	if err := sink.Update(context.Background(), "password=unredacted"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, ok := hub.Latest(taskID); ok {
		t.Fatal("unguarded stream snapshot was published")
	}
	if err := sink.Finalize(context.Background(), "password=[redacted]"); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	frame, ok := hub.Latest(taskID)
	if !ok {
		t.Fatal("guarded final frame was not published")
	}
	if !frame.Done || frame.Text != "password=[redacted]" {
		t.Fatalf("final frame = %#v", frame)
	}
}

func TestConsoleEventPreviewSinkPublishesBashTail(t *testing.T) {
	hub := newConsoleStreamHub()
	sink := newConsoleEventPreviewSink(hub, "task-preview", nil, nil)

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolStart,
		ToolName: "bash",
		Status:   "running",
	})
	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "bash",
		Stream:   "stdout",
		Text:     "alpha\n",
	})
	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "bash",
		Stream:   "stderr",
		Text:     "warn\n",
	})
	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolDone,
		ToolName: "bash",
		Status:   "done",
	})

	frame, ok := hub.Latest("task-preview")
	if !ok {
		t.Fatal("expected preview frame")
	}
	if !strings.Contains(frame.Text, "[bash] done") {
		t.Fatalf("frame.Text = %q, want bash done line", frame.Text)
	}
	if !strings.Contains(frame.Text, "stdout:\nalpha") {
		t.Fatalf("frame.Text = %q, want stdout tail", frame.Text)
	}
	if !strings.Contains(frame.Text, "stderr:\nwarn") {
		t.Fatalf("frame.Text = %q, want stderr tail", frame.Text)
	}
}

func TestConsoleEventPreviewSinkPublishesCoderActivityOutput(t *testing.T) {
	hub := newConsoleStreamHub()
	sink := newConsoleEventPreviewSink(hub, "task-coder", nil, nil)

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:       agent.EventKindToolStart,
		ActivityID: "tool:coder",
		ToolName:   "coder",
		Status:     "running",
	})
	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "coder",
		Stream:   "codex",
		Text:     "working",
		Status:   "running",
	})

	frame, ok := hub.Latest("task-coder")
	if !ok {
		t.Fatal("expected coder activity frame")
	}
	if frame.Activity == nil || frame.Activity.Current == nil {
		t.Fatalf("frame.Activity = %#v, want current activity", frame.Activity)
	}
	if frame.Activity.Current.Output != "working" {
		t.Fatalf("frame.Activity.Current.Output = %q, want working", frame.Activity.Current.Output)
	}
	if frame.Activity.Current.Stream != "codex" {
		t.Fatalf("frame.Activity.Current.Stream = %q, want codex", frame.Activity.Current.Stream)
	}
}

func TestConsoleEventPreviewSinkHidesRawToolOutputWhenGuardEnabled(t *testing.T) {
	hub := newConsoleStreamHub()
	outputGuard := guard.New(guard.Config{Enabled: true}, nil, nil)
	sink := newConsoleEventPreviewSink(hub, "task-guarded-tool", nil, outputGuard)

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:       agent.EventKindToolStart,
		ActivityID: "tool:coder",
		ToolName:   "coder",
		Status:     "running",
	})
	sink.HandleEvent(context.Background(), agent.Event{
		Kind:       agent.EventKindToolOutput,
		ActivityID: "tool:coder",
		ToolName:   "coder",
		Stream:     "codex",
		Text:       "password=unredacted",
		Status:     "running",
	})
	sink.HandleEvent(context.Background(), agent.Event{
		Kind:       agent.EventKindToolDone,
		ActivityID: "tool:coder",
		ToolName:   "coder",
		Status:     "failed",
		Error:      "password=unredacted",
	})

	frame, ok := hub.Latest("task-guarded-tool")
	if !ok {
		t.Fatal("expected status-only preview frame")
	}
	if strings.Contains(frame.Text, "unredacted") {
		t.Fatalf("preview leaked raw tool output: %q", frame.Text)
	}
	if frame.Activity != nil && frame.Activity.Current != nil && strings.Contains(frame.Activity.Current.Output, "unredacted") {
		t.Fatalf("activity leaked raw tool output: %#v", frame.Activity.Current)
	}
}

func TestConsoleStreamHubPublishesPlanFrame(t *testing.T) {
	hub := newConsoleStreamHub()
	taskID := "task-plan"

	hub.PublishPlan(taskID, &consolePlanProgress{
		Steps: []consolePlanStep{
			{Step: "scan repo", Status: agent.PlanStatusCompleted},
			{Step: "patch bug", Status: agent.PlanStatusInProgress},
		},
	})

	frame, ok := hub.Latest(taskID)
	if !ok {
		t.Fatal("expected plan frame")
	}
	if frame.Status != "running" {
		t.Fatalf("frame.Status = %q, want %q", frame.Status, "running")
	}
	if frame.Plan == nil {
		t.Fatal("frame.Plan = nil")
	}
	if len(frame.Plan.Steps) != 2 {
		t.Fatalf("len(frame.Plan.Steps) = %d, want 2", len(frame.Plan.Steps))
	}
	if frame.Plan.Steps[1].Status != agent.PlanStatusInProgress {
		t.Fatalf("frame.Plan.Steps[1].Status = %q, want %q", frame.Plan.Steps[1].Status, agent.PlanStatusInProgress)
	}
}

func TestConsoleStreamHubPublishesActivityFrame(t *testing.T) {
	hub := newConsoleStreamHub()
	taskID := "task-activity"

	hub.PublishActivity(taskID, &consoleActivityProgress{
		Current: &consoleActivityEntry{
			ID:     "tool:1",
			Kind:   "tool",
			Name:   "web_search",
			Status: "running",
			Args: map[string]any{
				"q": "alpha",
			},
		},
		History: []consoleActivityEntry{
			{
				ID:     "tool:1",
				Kind:   "tool",
				Name:   "web_search",
				Status: "running",
			},
		},
	})

	frame, ok := hub.Latest(taskID)
	if !ok {
		t.Fatal("expected activity frame")
	}
	if frame.Activity == nil || frame.Activity.Current == nil {
		t.Fatalf("frame.Activity = %#v, want current activity", frame.Activity)
	}
	if frame.Activity.Current.Name != "web_search" {
		t.Fatalf("frame.Activity.Current.Name = %q, want web_search", frame.Activity.Current.Name)
	}
	if frame.Activity.Current.Args["q"] != "alpha" {
		t.Fatalf("frame.Activity.Current.Args[q] = %#v, want alpha", frame.Activity.Current.Args["q"])
	}
}

func TestConsoleStreamHubPublishesPreviewFrame(t *testing.T) {
	hub := newConsoleStreamHub()
	taskID := "task-preview"

	hub.PublishPreview(taskID, "[web_search] done")

	frame, ok := hub.Latest(taskID)
	if !ok {
		t.Fatal("expected preview frame")
	}
	if !frame.Preview {
		t.Fatal("frame.Preview = false, want true")
	}
	if frame.Text != "[web_search] done" {
		t.Fatalf("frame.Text = %q, want %q", frame.Text, "[web_search] done")
	}
}

func TestConsoleEventPreviewSinkLongShellThrottlesOutput(t *testing.T) {
	hub := newConsoleStreamHub()
	sink := newConsoleEventPreviewSink(hub, "task-throttle", nil, nil)

	now := time.Unix(1000, 0)
	sink.now = func() time.Time { return now }

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolStart,
		ToolName: "bash",
		Status:   "running",
	})
	startFrame, ok := hub.Latest("task-throttle")
	if !ok {
		t.Fatal("expected initial throttle frame")
	}
	startSeq := startFrame.Seq

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "bash",
		Profile:  string(agent.ObserveProfileLongShell),
		Stream:   "stdout",
		Text:     "a",
	})
	firstOutputFrame, ok := hub.Latest("task-throttle")
	if !ok {
		t.Fatal("expected first throttle output frame")
	}
	firstOutputSeq := firstOutputFrame.Seq
	if firstOutputSeq <= startSeq {
		t.Fatalf("first output should publish immediately, startSeq=%d firstOutputSeq=%d", startSeq, firstOutputSeq)
	}

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "bash",
		Profile:  string(agent.ObserveProfileLongShell),
		Stream:   "stdout",
		Text:     "b",
	})
	currentFrame, ok := hub.Latest("task-throttle")
	if !ok {
		t.Fatal("expected throttle frame after small output")
	}
	if currentFrame.Seq != firstOutputSeq {
		t.Fatalf("small incremental output should not publish immediately")
	}

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "bash",
		Profile:  string(agent.ObserveProfileLongShell),
		Stream:   "stdout",
		Text:     strings.Repeat("x", 300),
	})
	currentFrame, ok = hub.Latest("task-throttle")
	if !ok {
		t.Fatal("expected throttle frame after large output")
	}
	if currentFrame.Seq == firstOutputSeq {
		t.Fatalf("large incremental output should trigger publish")
	}
}

func TestConsoleEventPreviewSinkWebExtractSuppressesRawOutput(t *testing.T) {
	hub := newConsoleStreamHub()
	sink := newConsoleEventPreviewSink(hub, "task-web", nil, nil)

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:    agent.EventKindSubtaskStart,
		TaskID:  "sub_web",
		Mode:    "agent",
		Profile: string(agent.ObserveProfileWebExtract),
		Status:  "running",
	})
	startFrame, ok := hub.Latest("task-web")
	if !ok {
		t.Fatal("expected web preview frame")
	}
	startSeq := startFrame.Seq

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "url_fetch",
		Stream:   "stdout",
		Text:     "<html>noise</html>",
	})
	currentFrame, ok := hub.Latest("task-web")
	if !ok {
		t.Fatal("expected web preview frame after suppressed output")
	}
	if currentFrame.Seq != startSeq {
		t.Fatalf("web_extract raw output should stay suppressed before terminal event")
	}
}

func TestConsoleEventPreviewSinkWebExtractSchedulesObserverSummary(t *testing.T) {
	hub := newConsoleStreamHub()
	sink := newConsoleEventPreviewSink(hub, "task-observe", nil, nil)
	sink.observer = stubConsoleSemanticObserver{summary: "Found candidate article list and narrowed the target."}
	defer sink.Close()

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:    agent.EventKindSubtaskStart,
		TaskID:  "sub_web",
		Mode:    "agent",
		Profile: string(agent.ObserveProfileWebExtract),
		Status:  "running",
	})
	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "url_fetch",
		Stream:   "stdout",
		Text:     "<html>noise</html>",
	})
	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolDone,
		ToolName: "url_fetch",
		Status:   "done",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		frame, ok := hub.Latest("task-observe")
		if ok && strings.Contains(frame.Text, "summary:\nFound candidate article list") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	frame, ok := hub.Latest("task-observe")
	if !ok {
		t.Fatal("expected observer preview frame")
	}
	t.Fatalf("observer summary did not appear, latest=%s", fmt.Sprintf("%#v", frame))
}
