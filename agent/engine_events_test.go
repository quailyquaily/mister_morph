package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type recordingEventSink struct {
	mu     sync.Mutex
	events []Event
}

func (s *recordingEventSink) HandleEvent(_ context.Context, event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *recordingEventSink) all() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

type contextAwareEventSink struct {
	recordingEventSink
}

func (s *contextAwareEventSink) HandleEvent(ctx context.Context, event Event) {
	if ctx != nil && ctx.Err() != nil {
		return
	}
	s.recordingEventSink.HandleEvent(ctx, event)
}

type cancelingClient struct {
	cancel context.CancelFunc
}

func (c cancelingClient) Chat(ctx context.Context, _ llm.Request) (llm.Result, error) {
	if c.cancel != nil {
		c.cancel()
	}
	<-ctx.Done()
	return llm.Result{}, ctx.Err()
}

func TestRun_EmitsTurnStartAndDone(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	engine := New(client, tools.NewRegistry(), Config{}, DefaultPromptSpec())
	sink := &recordingEventSink{}

	ctx := llmstats.WithRunID(context.Background(), "run_turn_done")
	ctx = WithEventSinkContext(ctx, sink)
	final, _, err := engine.Run(ctx, "test", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if final == nil || final.Output != "ok" {
		t.Fatalf("final = %#v, want output ok", final)
	}

	events := sink.all()
	kinds := eventKinds(events)
	if len(kinds) < 2 {
		t.Fatalf("events = %#v, want at least turn start/done", events)
	}
	if kinds[0] != EventKindTurnStart {
		t.Fatalf("first event kind = %q, want %q; events=%#v", kinds[0], EventKindTurnStart, kinds)
	}
	if kinds[len(kinds)-1] != EventKindTurnDone {
		t.Fatalf("last event kind = %q, want %q; events=%#v", kinds[len(kinds)-1], EventKindTurnDone, kinds)
	}
	for _, ev := range []Event{events[0], events[len(events)-1]} {
		if strings.TrimSpace(ev.RunID) != "run_turn_done" {
			t.Fatalf("event RunID = %q, want run_turn_done", ev.RunID)
		}
	}
}

func TestRun_EmitsTurnCanceledWithDetachedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := cancelingClient{cancel: cancel}
	engine := New(client, tools.NewRegistry(), Config{}, DefaultPromptSpec())
	sink := &contextAwareEventSink{}

	ctx = llmstats.WithRunID(ctx, "run_turn_detached_cancel")
	ctx = WithEventSinkContext(ctx, sink)

	_, _, err := engine.Run(ctx, "test", RunOptions{})
	if err == nil {
		t.Fatal("Run() error = nil, want cancellation error")
	}

	events := sink.all()
	if !eventsContainKind(events, EventKindTurnStart) {
		t.Fatalf("events = %#v, want turn_start", events)
	}
	if !eventsContainKind(events, EventKindTurnCanceled) {
		t.Fatalf("events = %#v, want turn_canceled even when run context is canceled", events)
	}
}

func TestRun_EmitsTurnCanceledWhenContextCanceled(t *testing.T) {
	client := newMockClient(llm.Result{})
	engine := New(client, tools.NewRegistry(), Config{}, DefaultPromptSpec())
	sink := &recordingEventSink{}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = llmstats.WithRunID(ctx, "run_turn_cancel")
	ctx = WithEventSinkContext(ctx, sink)
	cancel()

	_, _, err := engine.Run(ctx, "test", RunOptions{})
	if err == nil {
		t.Fatal("Run() error = nil, want cancellation error")
	}

	events := sink.all()
	kinds := eventKinds(events)
	if len(kinds) != 2 {
		t.Fatalf("events = %#v, want exactly turn start/canceled", events)
	}
	if kinds[0] != EventKindTurnStart {
		t.Fatalf("first event kind = %q, want %q", kinds[0], EventKindTurnStart)
	}
	if kinds[1] != EventKindTurnCanceled {
		t.Fatalf("second event kind = %q, want %q", kinds[1], EventKindTurnCanceled)
	}
	if strings.TrimSpace(events[1].RunID) != "run_turn_cancel" {
		t.Fatalf("cancel event RunID = %q, want run_turn_cancel", events[1].RunID)
	}
}

func eventKinds(events []Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Kind)
	}
	return out
}
