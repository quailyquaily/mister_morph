package streaming

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

func TestExtractPartialFinalOutput(t *testing.T) {
	t.Run("extracts partial output string", func(t *testing.T) {
		got := ExtractPartialFinalOutput(`{"type":"final","reasoning":"x","output":"hel`)
		if got.ResponseType != "final" {
			t.Fatalf("ResponseType = %q, want final", got.ResponseType)
		}
		if !got.OutputStarted {
			t.Fatalf("OutputStarted = false, want true")
		}
		if got.Output != "hel" {
			t.Fatalf("Output = %q, want hel", got.Output)
		}
		if got.OutputComplete {
			t.Fatalf("OutputComplete = true, want false")
		}
	})

	t.Run("decodes escaped output", func(t *testing.T) {
		got := ExtractPartialFinalOutput(`{"type":"final","output":"hi\nthere \u4f60\u597d"}`)
		if got.Output != "hi\nthere 你好" {
			t.Fatalf("Output = %q", got.Output)
		}
		if !got.OutputComplete {
			t.Fatalf("OutputComplete = false, want true")
		}
	})

	t.Run("ignores non final responses", func(t *testing.T) {
		got := ExtractPartialFinalOutput(`{"type":"plan","steps":[{"step":"a"}]}`)
		if got.OutputStarted {
			t.Fatalf("OutputStarted = true, want false")
		}
	})
}

func TestFinalOutputStreamer(t *testing.T) {
	now := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	sink := &recordingReplySink{}
	streamer := NewFinalOutputStreamer(FinalOutputStreamerOptions{
		Sink:        sink,
		MinInterval: 250 * time.Millisecond,
		Now: func() time.Time {
			return now
		},
	})

	if err := streamer.Handle(llm.StreamEvent{Delta: `{"type":"final","output":"he`}); err != nil {
		t.Fatalf("Handle first delta: %v", err)
	}
	if len(sink.updates) != 1 || sink.updates[0] != "he" {
		t.Fatalf("updates after first delta = %#v", sink.updates)
	}

	now = now.Add(100 * time.Millisecond)
	if err := streamer.Handle(llm.StreamEvent{Delta: `llo`}); err != nil {
		t.Fatalf("Handle second delta: %v", err)
	}
	if len(sink.updates) != 1 {
		t.Fatalf("updates after throttled delta = %#v", sink.updates)
	}

	now = now.Add(100 * time.Millisecond)
	if err := streamer.Handle(llm.StreamEvent{Delta: `"}`}); err != nil {
		t.Fatalf("Handle third delta: %v", err)
	}
	if len(sink.updates) != 1 {
		t.Fatalf("updates before done flush = %#v", sink.updates)
	}

	now = now.Add(10 * time.Millisecond)
	if err := streamer.Handle(llm.StreamEvent{Done: true}); err != nil {
		t.Fatalf("Handle done: %v", err)
	}
	if len(sink.updates) != 2 || sink.updates[1] != "hello" {
		t.Fatalf("updates after done flush = %#v", sink.updates)
	}
}

func TestFinalOutputStreamerPropagatesSinkError(t *testing.T) {
	wantErr := errors.New("boom")
	streamer := NewFinalOutputStreamer(FinalOutputStreamerOptions{
		Sink: &recordingReplySink{updateErr: wantErr},
	})
	err := streamer.Handle(llm.StreamEvent{Delta: `{"type":"final","output":"x`})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Handle error = %v, want %v", err, wantErr)
	}
}

type recordingReplySink struct {
	updates   []string
	finals    []string
	aborts    []error
	updateErr error
}

func (s *recordingReplySink) Update(_ context.Context, text string) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updates = append(s.updates, text)
	return nil
}

func (s *recordingReplySink) Finalize(_ context.Context, text string) error {
	s.finals = append(s.finals, text)
	return nil
}

func (s *recordingReplySink) Abort(_ context.Context, err error) error {
	s.aborts = append(s.aborts, err)
	return nil
}
