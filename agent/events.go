package agent

import (
	"context"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmstats"
)

const (
	EventKindToolStart    = "tool_start"
	EventKindToolDone     = "tool_done"
	EventKindToolOutput   = "tool_output"
	EventKindSubtaskStart = "subtask_start"
	EventKindSubtaskDone  = "subtask_done"
)

type Event struct {
	Kind       string `json:"kind"`
	RunID      string `json:"run_id,omitempty"`
	Step       int    `json:"step,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	Status     string `json:"status,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Profile    string `json:"profile,omitempty"`
	Stream     string `json:"stream,omitempty"`
	Text       string `json:"text,omitempty"`
	Summary    string `json:"summary,omitempty"`
	OutputKind string `json:"output_kind,omitempty"`
	Error      string `json:"error,omitempty"`
}

type EventSink interface {
	HandleEvent(context.Context, Event)
}

type EventSinkFunc func(context.Context, Event)

func (fn EventSinkFunc) HandleEvent(ctx context.Context, event Event) {
	if fn != nil {
		fn(ctx, event)
	}
}

type eventSinkContextKey struct{}

func WithEventSinkContext(ctx context.Context, sink EventSink) context.Context {
	if sink == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, eventSinkContextKey{}, sink)
}

func EventSinkFromContext(ctx context.Context) (EventSink, bool) {
	if ctx == nil {
		return nil, false
	}
	sink, ok := ctx.Value(eventSinkContextKey{}).(EventSink)
	return sink, ok && sink != nil
}

func EmitEvent(ctx context.Context, sink EventSink, event Event) {
	if sink == nil {
		var ok bool
		sink, ok = EventSinkFromContext(ctx)
		if !ok {
			return
		}
	}
	if strings.TrimSpace(event.RunID) == "" {
		event.RunID = strings.TrimSpace(llmstats.RunIDFromContext(ctx))
	}
	sink.HandleEvent(ctx, event)
}
