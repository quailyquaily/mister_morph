package consolecmd

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
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
	if _, ok := hub.latest[taskID]; !ok {
		t.Fatal("latest snapshot missing before completion")
	}

	hub.PublishFinal(taskID, "final output")
	if _, ok := hub.latest[taskID]; ok {
		t.Fatal("latest entry retained after done frame without subscribers")
	}
}

func TestConsoleStreamHubEvictsDoneFrameOnLastUnsubscribe(t *testing.T) {
	hub := newConsoleStreamHub()
	taskID := "task-with-subscriber"

	_, unsubscribe := hub.Subscribe(taskID)
	hub.PublishFinal(taskID, "final output")

	latest, ok := hub.latest[taskID]
	if !ok {
		t.Fatal("latest entry missing while subscriber is connected")
	}
	if !latest.Done {
		t.Fatalf("latest.Done = %v, want true", latest.Done)
	}

	unsubscribe()
	if _, ok := hub.latest[taskID]; ok {
		t.Fatal("latest entry retained after last subscriber unsubscribed")
	}
}

func TestConsoleEventPreviewSinkPublishesBashTail(t *testing.T) {
	hub := newConsoleStreamHub()
	sink := newConsoleEventPreviewSink(hub, "task-preview", nil)

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

	frame, ok := hub.latest["task-preview"]
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

func TestConsoleEventPreviewSinkLongShellThrottlesOutput(t *testing.T) {
	hub := newConsoleStreamHub()
	sink := newConsoleEventPreviewSink(hub, "task-throttle", nil)

	now := time.Unix(1000, 0)
	sink.now = func() time.Time { return now }

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolStart,
		ToolName: "bash",
		Status:   "running",
	})
	startSeq := hub.latest["task-throttle"].Seq

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "bash",
		Profile:  string(agent.ObserveProfileLongShell),
		Stream:   "stdout",
		Text:     "a",
	})
	firstOutputSeq := hub.latest["task-throttle"].Seq
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
	if hub.latest["task-throttle"].Seq != firstOutputSeq {
		t.Fatalf("small incremental output should not publish immediately")
	}

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "bash",
		Profile:  string(agent.ObserveProfileLongShell),
		Stream:   "stdout",
		Text:     strings.Repeat("x", 300),
	})
	if hub.latest["task-throttle"].Seq == firstOutputSeq {
		t.Fatalf("large incremental output should trigger publish")
	}
}

func TestConsoleEventPreviewSinkWebExtractSuppressesRawOutput(t *testing.T) {
	hub := newConsoleStreamHub()
	sink := newConsoleEventPreviewSink(hub, "task-web", nil)

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:    agent.EventKindSubtaskStart,
		TaskID:  "sub_web",
		Mode:    "agent",
		Profile: string(agent.ObserveProfileWebExtract),
		Status:  "running",
	})
	startSeq := hub.latest["task-web"].Seq

	sink.HandleEvent(context.Background(), agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "url_fetch",
		Stream:   "stdout",
		Text:     "<html>noise</html>",
	})
	if hub.latest["task-web"].Seq != startSeq {
		t.Fatalf("web_extract raw output should stay suppressed before terminal event")
	}
}

func TestConsoleEventPreviewSinkWebExtractSchedulesObserverSummary(t *testing.T) {
	hub := newConsoleStreamHub()
	sink := newConsoleEventPreviewSink(hub, "task-observe", nil)
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
		frame, ok := hub.latest["task-observe"]
		if ok && strings.Contains(frame.Text, "summary:\nFound candidate article list") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	frame, ok := hub.latest["task-observe"]
	if !ok {
		t.Fatal("expected observer preview frame")
	}
	t.Fatalf("observer summary did not appear, latest=%s", fmt.Sprintf("%#v", frame))
}
