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
	if s.closed {
		return nil
	}
	if len(s.batches) == 0 {
		return nil
	}
	out := s.batches[0]
	s.batches = s.batches[1:]
	return out
}

func (s *scriptedSteerSource) DrainAndClose() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
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

func TestSteerSourceExposesCloseOperations(t *testing.T) {
	source := SteerSource(&scriptedSteerSource{
		batches: [][]string{{"final note"}},
	})

	items := source.DrainAndClose()
	if len(items) != 1 || items[0] != "final note" {
		t.Fatalf("DrainAndClose() = %#v, want final note", items)
	}
	source.Close()
}

type pushableSteerSource struct {
	mu     sync.Mutex
	items  []string
	closed bool
}

func (s *pushableSteerSource) Drain() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) == 0 {
		return nil
	}
	out := make([]string, len(s.items))
	copy(out, s.items)
	s.items = nil
	return out
}

func (s *pushableSteerSource) DrainAndClose() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if len(s.items) == 0 {
		return nil
	}
	out := make([]string, len(s.items))
	copy(out, s.items)
	s.items = nil
	return out
}

func (s *pushableSteerSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.items = nil
}

func (s *pushableSteerSource) Push(input string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return context.Canceled
	}
	s.items = append(s.items, input)
	return nil
}

type forceConclusionPushClient struct {
	steer   *pushableSteerSource
	pushErr error

	mu    sync.Mutex
	calls []llm.Request
}

func (c *forceConclusionPushClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.mu.Lock()
	c.calls = append(c.calls, req)
	callIndex := len(c.calls)
	c.mu.Unlock()

	if callIndex == 1 {
		return llm.Result{Text: `{"type":"plan","steps":["inspect"]}`}, nil
	}
	c.pushErr = c.steer.Push("too late for force conclusion")
	return finalResponse("forced"), nil
}

func (c *forceConclusionPushClient) allCalls() []llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]llm.Request, len(c.calls))
	copy(out, c.calls)
	return out
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

func TestRun_RejectsSteerDuringForceConclusionCall(t *testing.T) {
	steer := &pushableSteerSource{}
	client := &forceConclusionPushClient{steer: steer}
	engine := New(client, baseRegistry(), Config{MaxSteps: 1}, DefaultPromptSpec())

	final, _, err := engine.Run(context.Background(), "test", RunOptions{SteerSource: steer})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got, _ := final.Output.(string)
	if final == nil || strings.TrimSpace(got) != "forced" {
		t.Fatalf("final = %#v, want forced", final)
	}
	if client.pushErr == nil {
		t.Fatal("Push() during forceConclusion succeeded; steer would be acknowledged but never consumed")
	}
	if calls := client.allCalls(); len(calls) != 2 {
		t.Fatalf("calls = %d, want plan call and force conclusion call", len(calls))
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
