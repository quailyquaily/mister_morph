package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

type scriptedSteerSource struct {
	mu      sync.Mutex
	batches [][]string
	closed  bool
}

func (s *scriptedSteerSource) Drain() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.batches) == 0 {
		return nil
	}
	out := s.batches[0]
	s.batches = s.batches[1:]
	return out
}

func (s *scriptedSteerSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

func TestRun_AppliesQueuedSteerBeforeLLMCall(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	engine := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())
	sink := &recordingEventSink{}
	steer := &scriptedSteerSource{
		batches: [][]string{{"prefer a short answer"}},
	}
	ctx := WithEventSinkContext(context.Background(), sink)

	_, _, err := engine.Run(ctx, "test", RunOptions{SteerSource: steer})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	calls := client.allCalls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if !requestContains(calls, 0, "prefer a short answer") {
		t.Fatalf("first request did not include steer: %#v", calls[0].Messages)
	}
	if !eventsContainKind(sink.all(), EventKindSteerApplied) {
		t.Fatalf("events missing steer_applied: %#v", sink.all())
	}
}

func TestRun_ClosesSteerSourceWhenDone(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	engine := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())
	steer := &scriptedSteerSource{}

	_, _, err := engine.Run(context.Background(), "test", RunOptions{SteerSource: steer})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	steer.mu.Lock()
	closed := steer.closed
	steer.mu.Unlock()
	if !closed {
		t.Fatal("steer source was not closed after Run()")
	}
}

func TestRun_AppliesQueuedSteerBeforeAcceptingFinal(t *testing.T) {
	client := newMockClient(
		finalResponse("draft"),
		finalResponse("revised"),
	)
	engine := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())
	steer := &scriptedSteerSource{
		batches: [][]string{
			nil,
			{"revise with risk notes"},
			nil,
		},
	}

	final, _, err := engine.Run(context.Background(), "test", RunOptions{SteerSource: steer})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got, _ := final.Output.(string)
	if final == nil || strings.TrimSpace(got) != "revised" {
		t.Fatalf("final = %#v, want revised", final)
	}

	calls := client.allCalls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if !requestContains(calls, 1, "revise with risk notes") {
		t.Fatalf("second request did not include final-stage steer: %#v", calls[1].Messages)
	}
}

func TestRun_AppliesQueuedSteerBeforeForceConclusion(t *testing.T) {
	client := newMockClient(
		llm.Result{Text: `{"type":"plan","steps":["inspect"]}`},
		finalResponse("forced"),
	)
	engine := New(client, baseRegistry(), Config{MaxSteps: 1}, DefaultPromptSpec())
	steer := &scriptedSteerSource{
		batches: [][]string{
			nil,
			{"include the late correction"},
		},
	}

	final, _, err := engine.Run(context.Background(), "test", RunOptions{SteerSource: steer})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got, _ := final.Output.(string)
	if final == nil || strings.TrimSpace(got) != "forced" {
		t.Fatalf("final = %#v, want forced", final)
	}

	calls := client.allCalls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if !requestContains(calls, 1, "include the late correction") {
		t.Fatalf("force conclusion request did not include late steer: %#v", calls[1].Messages)
	}
}

func eventsContainKind(events []Event, kind string) bool {
	for _, ev := range events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}
