package agent

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

type capturedLogEntry struct {
	message string
	attrs   map[string]any
}

type captureLogSink struct {
	mu      sync.Mutex
	entries []capturedLogEntry
}

type captureLogHandler struct {
	sink  *captureLogSink
	attrs []slog.Attr
}

func newCaptureLogger() (*slog.Logger, *captureLogSink) {
	sink := &captureLogSink{}
	return slog.New(&captureLogHandler{sink: sink}), sink
}

func (h *captureLogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *captureLogHandler) Handle(_ context.Context, rec slog.Record) error {
	attrs := make(map[string]any, len(h.attrs)+rec.NumAttrs())
	for _, attr := range h.attrs {
		attrs[attr.Key] = attr.Value.Any()
	}
	rec.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	h.sink.mu.Lock()
	defer h.sink.mu.Unlock()
	h.sink.entries = append(h.sink.entries, capturedLogEntry{
		message: rec.Message,
		attrs:   attrs,
	})
	return nil
}

func (h *captureLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &captureLogHandler{sink: h.sink}
	next.attrs = append(next.attrs, h.attrs...)
	next.attrs = append(next.attrs, attrs...)
	return next
}

func (h *captureLogHandler) WithGroup(string) slog.Handler {
	return h
}

func (s *captureLogSink) entry(message string) (capturedLogEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.entries {
		if entry.message == message {
			return entry, true
		}
	}
	return capturedLogEntry{}, false
}

func TestToolDoneLogDoesNotInheritRunModel(t *testing.T) {
	logger, sink := newCaptureLogger()

	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "image_generate", result: `{"ok":true}`})
	client := newMockClient(
		toolCallResponse("image_generate"),
		finalResponse("done"),
	)
	cfg := baseCfg()
	cfg.DefaultModel = "gemma-4-31b-it-ud-q4-k-xl"
	engine := New(client, reg, cfg, DefaultPromptSpec(), WithLogger(logger))

	if _, _, err := engine.Run(context.Background(), "generate image", RunOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	runStart, ok := sink.entry("run_start")
	if !ok {
		t.Fatal("missing run_start log")
	}
	if got := runStart.attrs["model"]; got != cfg.DefaultModel {
		t.Fatalf("run_start model = %#v, want %q", got, cfg.DefaultModel)
	}

	toolDone, ok := sink.entry("tool_done")
	if !ok {
		t.Fatal("missing tool_done log")
	}
	if _, ok := toolDone.attrs["model"]; ok {
		t.Fatalf("tool_done should not include run model attr: %#v", toolDone.attrs)
	}
	if got := toolDone.attrs["tool"]; got != "image_generate" {
		t.Fatalf("tool_done tool = %#v, want image_generate", got)
	}
}
