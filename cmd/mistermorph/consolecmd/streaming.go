package consolecmd

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/quailyquaily/mistermorph/llm"
)

const consoleStreamTicketTTL = 60 * time.Second

type consoleStreamFrame struct {
	TaskID string `json:"task_id"`
	Seq    uint64 `json:"seq"`
	Status string `json:"status,omitempty"`
	Text   string `json:"text,omitempty"`
	Error  string `json:"error,omitempty"`
	Done   bool   `json:"done,omitempty"`
}

type consoleStreamHub struct {
	mu      sync.RWMutex
	nextSeq uint64
	latest  map[string]consoleStreamFrame
	subs    map[string]map[chan consoleStreamFrame]struct{}
}

func newConsoleStreamHub() *consoleStreamHub {
	return &consoleStreamHub{
		latest: map[string]consoleStreamFrame{},
		subs:   map[string]map[chan consoleStreamFrame]struct{}{},
	}
}

func (h *consoleStreamHub) PublishSnapshot(taskID, text string) {
	h.publish(consoleStreamFrame{
		TaskID: strings.TrimSpace(taskID),
		Status: "running",
		Text:   text,
	})
}

func (h *consoleStreamHub) PublishFinal(taskID, text string) {
	h.publish(consoleStreamFrame{
		TaskID: strings.TrimSpace(taskID),
		Status: "done",
		Text:   text,
		Done:   true,
	})
}

func (h *consoleStreamHub) PublishAbort(taskID, text string) {
	h.publish(consoleStreamFrame{
		TaskID: strings.TrimSpace(taskID),
		Status: "failed",
		Text:   text,
		Error:  text,
		Done:   true,
	})
}

func (h *consoleStreamHub) PublishStatus(taskID, status string) {
	h.publish(consoleStreamFrame{
		TaskID: strings.TrimSpace(taskID),
		Status: strings.TrimSpace(status),
	})
}

func (h *consoleStreamHub) Subscribe(taskID string) (<-chan consoleStreamFrame, func()) {
	if h == nil {
		ch := make(chan consoleStreamFrame)
		close(ch)
		return ch, func() {}
	}
	taskID = strings.TrimSpace(taskID)
	ch := make(chan consoleStreamFrame, 4)

	h.mu.Lock()
	if h.subs[taskID] == nil {
		h.subs[taskID] = map[chan consoleStreamFrame]struct{}{}
	}
	h.subs[taskID][ch] = struct{}{}
	latest, hasLatest := h.latest[taskID]
	h.mu.Unlock()

	if hasLatest {
		ch <- latest
	}

	return ch, func() {
		h.mu.Lock()
		if subs := h.subs[taskID]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(h.subs, taskID)
				if latest, ok := h.latest[taskID]; ok && latest.Done {
					delete(h.latest, taskID)
				}
			}
		}
		h.mu.Unlock()
	}
}

func (h *consoleStreamHub) publish(frame consoleStreamFrame) {
	if h == nil || strings.TrimSpace(frame.TaskID) == "" {
		return
	}

	h.mu.Lock()
	h.nextSeq++
	frame.Seq = h.nextSeq

	subs := make([]chan consoleStreamFrame, 0, len(h.subs[frame.TaskID]))
	for sub := range h.subs[frame.TaskID] {
		subs = append(subs, sub)
	}
	if frame.Done && len(subs) == 0 {
		delete(h.latest, frame.TaskID)
	} else {
		h.latest[frame.TaskID] = frame
	}
	h.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub <- frame:
		default:
			select {
			case <-sub:
			default:
			}
			select {
			case sub <- frame:
			default:
			}
		}
	}
}

type consoleReplySink struct {
	hub    *consoleStreamHub
	taskID string
	logger *slog.Logger

	mu        sync.Mutex
	snapshots int
}

func newConsoleReplySink(hub *consoleStreamHub, taskID string, logger *slog.Logger) *consoleReplySink {
	return &consoleReplySink{
		hub:    hub,
		taskID: strings.TrimSpace(taskID),
		logger: logger,
	}
}

func (s *consoleReplySink) Update(_ context.Context, text string) error {
	if s == nil || s.hub == nil {
		return nil
	}
	s.mu.Lock()
	s.snapshots++
	snapshotCount := s.snapshots
	s.mu.Unlock()
	if s.logger != nil {
		fields := []any{
			"task_id", s.taskID,
			"snapshot_count", snapshotCount,
			"chars", utf8.RuneCountInString(text),
		}
		if snapshotCount == 1 {
			s.logger.Info("console_stream_first_snapshot", fields...)
		} else {
			s.logger.Debug("console_stream_snapshot", fields...)
		}
	}
	s.hub.PublishSnapshot(s.taskID, text)
	return nil
}

func (s *consoleReplySink) Finalize(_ context.Context, text string) error {
	if s == nil || s.hub == nil {
		return nil
	}
	s.mu.Lock()
	snapshotCount := s.snapshots
	s.mu.Unlock()
	if s.logger != nil {
		s.logger.Info("console_stream_finalize",
			"task_id", s.taskID,
			"snapshots", snapshotCount,
			"streamed", snapshotCount > 0,
			"chars", utf8.RuneCountInString(text),
		)
	}
	s.hub.PublishFinal(s.taskID, text)
	return nil
}

func (s *consoleReplySink) Abort(_ context.Context, err error) error {
	if s == nil || s.hub == nil || err == nil {
		return nil
	}
	s.mu.Lock()
	snapshotCount := s.snapshots
	s.mu.Unlock()
	if s.logger != nil {
		s.logger.Warn("console_stream_abort",
			"task_id", s.taskID,
			"snapshots", snapshotCount,
			"error", strings.TrimSpace(err.Error()),
		)
	}
	s.hub.PublishAbort(s.taskID, strings.TrimSpace(err.Error()))
	return nil
}

type consoleStreamTracker struct {
	logger *slog.Logger
	taskID string

	mu       sync.Mutex
	events   int
	rawBytes int
}

func newConsoleStreamTracker(logger *slog.Logger, taskID string) *consoleStreamTracker {
	return &consoleStreamTracker{
		logger: logger,
		taskID: strings.TrimSpace(taskID),
	}
}

func (t *consoleStreamTracker) Handle(event llm.StreamEvent, next func(llm.StreamEvent) error) error {
	if t != nil {
		t.observe(event)
	}
	if next != nil {
		return next(event)
	}
	return nil
}

func (t *consoleStreamTracker) observe(event llm.StreamEvent) {
	if t == nil || t.logger == nil {
		return
	}
	t.mu.Lock()
	shouldCount := event.Delta != "" || event.ToolCallDelta != nil || event.Done
	if !shouldCount {
		t.mu.Unlock()
		return
	}
	t.events++
	t.rawBytes += len(event.Delta)
	eventCount := t.events
	rawBytes := t.rawBytes
	t.mu.Unlock()

	if eventCount == 1 {
		t.logger.Info("console_stream_first_delta",
			"task_id", t.taskID,
			"delta_bytes", len(event.Delta),
			"has_tool_call_delta", event.ToolCallDelta != nil,
			"done", event.Done,
		)
	}
	if event.Done {
		t.logger.Info("console_stream_done_signal",
			"task_id", t.taskID,
			"raw_events", eventCount,
			"raw_bytes", rawBytes,
		)
	}
}

func (t *consoleStreamTracker) LogSummary(outcome string) {
	if t == nil || t.logger == nil {
		return
	}
	t.mu.Lock()
	eventCount := t.events
	rawBytes := t.rawBytes
	t.mu.Unlock()
	t.logger.Info("console_stream_summary",
		"task_id", t.taskID,
		"outcome", strings.TrimSpace(outcome),
		"raw_events", eventCount,
		"raw_bytes", rawBytes,
		"streamed", eventCount > 0,
	)
}

func (s *server) handleStreamTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s == nil || s.streamTickets == nil {
		writeError(w, http.StatusServiceUnavailable, "stream ticket store unavailable")
		return
	}
	ticket, expiresAt, err := s.streamTickets.Create(consoleStreamTicketTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create stream ticket")
		return
	}
	if s.localRuntime != nil && s.localRuntime.logger != nil {
		s.localRuntime.logger.Debug("console_stream_ticket_created",
			"expires_at", expiresAt.Format(time.RFC3339Nano),
		)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":     ticket,
		"expires_at": expiresAt.Format(time.RFC3339Nano),
	})
}

func (s *server) handleStreamWebSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s == nil || s.localRuntime == nil || s.localRuntime.streamHub == nil {
		writeError(w, http.StatusServiceUnavailable, "stream is unavailable")
		return
	}

	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
	if ticket == "" || taskID == "" {
		writeError(w, http.StatusBadRequest, "missing ticket or task_id")
		return
	}
	if _, ok := s.streamTickets.Validate(ticket); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.streamTickets.Delete(ticket)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return sameOriginRequest(r)
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	if s.localRuntime != nil && s.localRuntime.logger != nil {
		s.localRuntime.logger.Info("console_stream_ws_connected",
			"task_id", taskID,
			"remote_addr", strings.TrimSpace(r.RemoteAddr),
		)
		defer s.localRuntime.logger.Info("console_stream_ws_disconnected",
			"task_id", taskID,
			"remote_addr", strings.TrimSpace(r.RemoteAddr),
		)
	}

	frames, unsubscribe := s.localRuntime.streamHub.Subscribe(taskID)
	defer unsubscribe()

	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(frame); err != nil {
				return
			}
		case <-pingTicker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-readDone:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}
