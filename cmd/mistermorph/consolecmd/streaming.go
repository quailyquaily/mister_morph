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
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/llm"
)

const consoleStreamTicketTTL = 60 * time.Second

type runtimeEndpointStreamClient interface {
	OpenTaskStream(ctx context.Context, taskID string) (*websocket.Conn, error)
}

type consoleStreamFrame struct {
	TaskID    string                   `json:"task_id"`
	Seq       uint64                   `json:"seq"`
	Status    string                   `json:"status,omitempty"`
	Text      string                   `json:"text,omitempty"`
	Reasoning string                   `json:"reasoning,omitempty"`
	Error     string                   `json:"error,omitempty"`
	Plan      *consolePlanProgress     `json:"plan,omitempty"`
	Activity  *consoleActivityProgress `json:"activity,omitempty"`
	Preview   bool                     `json:"preview,omitempty"`
	Done      bool                     `json:"done,omitempty"`
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

func (h *consoleStreamHub) PublishPreview(taskID, text string) {
	h.publish(consoleStreamFrame{
		TaskID:  strings.TrimSpace(taskID),
		Status:  "running",
		Text:    text,
		Preview: true,
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

func (h *consoleStreamHub) PublishPlan(taskID string, plan *consolePlanProgress) {
	if plan == nil {
		return
	}
	h.publish(consoleStreamFrame{
		TaskID: strings.TrimSpace(taskID),
		Status: "running",
		Plan:   plan,
	})
}

func (h *consoleStreamHub) PublishActivity(taskID string, activity *consoleActivityProgress) {
	if activity == nil {
		return
	}
	h.publish(consoleStreamFrame{
		TaskID:   strings.TrimSpace(taskID),
		Status:   "running",
		Activity: cloneConsoleActivityProgress(activity),
	})
}

func (h *consoleStreamHub) PublishReasoning(taskID, reasoning string) {
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" {
		return
	}
	h.publish(consoleStreamFrame{
		TaskID:    strings.TrimSpace(taskID),
		Status:    "running",
		Reasoning: reasoning,
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
	if hasLatest {
		ch <- latest
	}
	h.mu.Unlock()

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

func (h *consoleStreamHub) Latest(taskID string) (consoleStreamFrame, bool) {
	if h == nil {
		return consoleStreamFrame{}, false
	}
	taskID = strings.TrimSpace(taskID)

	h.mu.RLock()
	frame, ok := h.latest[taskID]
	h.mu.RUnlock()
	return frame, ok
}

func (h *consoleStreamHub) publish(frame consoleStreamFrame) {
	if h == nil || strings.TrimSpace(frame.TaskID) == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextSeq++
	frame.Seq = h.nextSeq
	previous := h.latest[frame.TaskID]
	if frame.Plan == nil {
		frame.Plan = previous.Plan
	}
	if frame.Activity == nil {
		frame.Activity = previous.Activity
	}
	if frame.Reasoning == "" {
		frame.Reasoning = previous.Reasoning
	}
	if frame.Text == "" && !frame.Done {
		frame.Text = previous.Text
		frame.Preview = previous.Preview
	}

	subs := h.subs[frame.TaskID]
	if frame.Done && len(subs) == 0 {
		delete(h.latest, frame.TaskID)
	} else {
		h.latest[frame.TaskID] = frame
	}
	for sub := range subs {
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
	hub            *consoleStreamHub
	taskID         string
	logger         *slog.Logger
	deferSnapshots bool

	mu        sync.Mutex
	snapshots int
}

type consoleReasoningSink struct {
	hub         *consoleStreamHub
	taskID      string
	outputGuard *guard.Guard
	now         func() time.Time

	mu             sync.Mutex
	buffer         strings.Builder
	lastBlockIndex int
	lastBlockType  llm.ReasoningDeltaType
	hasBlock       bool
	separateOnNext bool
	lastText       string
	lastEmitAt     time.Time
}

func newConsoleReasoningSink(hub *consoleStreamHub, taskID string, outputGuard *guard.Guard) *consoleReasoningSink {
	return &consoleReasoningSink{
		hub:         hub,
		taskID:      strings.TrimSpace(taskID),
		outputGuard: outputGuard,
		now:         time.Now,
	}
}

func (s *consoleReasoningSink) Handle(event llm.StreamEvent) error {
	if s == nil || s.hub == nil {
		return nil
	}

	var emitText string
	s.mu.Lock()
	if delta := event.ReasoningDelta; delta != nil && delta.Delta != "" {
		newBlock := s.separateOnNext ||
			(s.hasBlock && (s.lastBlockIndex != delta.Index || s.lastBlockType != delta.Type))
		if newBlock {
			current := s.buffer.String()
			switch {
			case current == "", strings.HasSuffix(current, "\n\n"):
			case strings.HasSuffix(current, "\n"):
				s.buffer.WriteByte('\n')
			default:
				s.buffer.WriteString("\n\n")
			}
		}
		if s.separateOnNext {
			s.separateOnNext = false
			s.lastEmitAt = time.Time{}
		}
		s.lastBlockIndex = delta.Index
		s.lastBlockType = delta.Type
		s.hasBlock = true
		s.buffer.WriteString(delta.Delta)

		current := strings.TrimSpace(s.buffer.String())
		now := time.Now().UTC()
		if s.now != nil {
			now = s.now().UTC()
		}
		if current != "" && current != s.lastText &&
			(s.lastEmitAt.IsZero() || now.Sub(s.lastEmitAt) >= 250*time.Millisecond) {
			s.lastText = current
			s.lastEmitAt = now
			emitText = current
		}
	}
	if event.Done {
		current := strings.TrimSpace(s.buffer.String())
		if current != "" && current != s.lastText {
			s.lastText = current
			if s.now != nil {
				s.lastEmitAt = s.now().UTC()
			} else {
				s.lastEmitAt = time.Now().UTC()
			}
			emitText = current
		}
		s.separateOnNext = true
	}
	s.mu.Unlock()

	if emitText != "" {
		if s.outputGuard != nil {
			emitText, _ = s.outputGuard.RedactString(emitText)
		}
		s.hub.PublishReasoning(s.taskID, emitText)
	}
	return nil
}

func (s *consoleReasoningSink) Snapshot() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	text := strings.TrimSpace(s.buffer.String())
	s.mu.Unlock()
	if s.outputGuard != nil {
		text, _ = s.outputGuard.RedactString(text)
	}
	return text
}

func newConsoleReplySink(hub *consoleStreamHub, taskID string, logger *slog.Logger, outputGuard *guard.Guard) *consoleReplySink {
	return &consoleReplySink{
		hub:            hub,
		taskID:         strings.TrimSpace(taskID),
		logger:         logger,
		deferSnapshots: outputGuard != nil && outputGuard.Enabled(),
	}
}

func (s *consoleReplySink) Update(_ context.Context, text string) error {
	if s == nil || s.hub == nil || s.deferSnapshots {
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
	reasoningBytes := 0
	if event.ReasoningDelta != nil {
		reasoningBytes = len(event.ReasoningDelta.Delta)
	}
	shouldCount := event.Delta != "" || reasoningBytes > 0 || event.ToolCallDelta != nil || event.Done
	if !shouldCount {
		t.mu.Unlock()
		return
	}
	t.events++
	t.rawBytes += len(event.Delta) + reasoningBytes
	eventCount := t.events
	rawBytes := t.rawBytes
	t.mu.Unlock()

	if eventCount == 1 {
		t.logger.Info("console_stream_first_delta",
			"task_id", t.taskID,
			"delta_bytes", len(event.Delta),
			"reasoning_delta_bytes", reasoningBytes,
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
	if s.localRuntime != nil {
		s.localRuntime.currentLogger().Debug("console_stream_ticket_created",
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
	if s == nil || s.streamTickets == nil {
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

	endpointRef := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	var frames <-chan consoleStreamFrame
	var closeFrames func()
	if endpointRef == "" || endpointRef == consoleLocalEndpointRef {
		if s.localRuntime == nil || s.localRuntime.streamHub == nil {
			writeError(w, http.StatusServiceUnavailable, "stream is unavailable")
			return
		}
		frames, closeFrames = s.localRuntime.streamHub.Subscribe(taskID)
	} else {
		endpoint, ok := s.endpointByRef[endpointRef]
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid endpoint")
			return
		}
		streamClient, ok := endpoint.Client.(runtimeEndpointStreamClient)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "endpoint stream is unavailable")
			return
		}
		upstream, err := streamClient.OpenTaskStream(r.Context(), taskID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		frames, closeFrames = relayRemoteStreamFrames(upstream, taskID)
	}
	defer closeFrames()

	s.serveStreamWebSocket(w, r, taskID, frames)
}

func (s *server) handleRuntimeStreamWebSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s == nil || s.localRuntime == nil || s.localRuntime.streamHub == nil {
		writeError(w, http.StatusServiceUnavailable, "stream is unavailable")
		return
	}
	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing task_id")
		return
	}
	frames, unsubscribe := s.localRuntime.streamHub.Subscribe(taskID)
	defer unsubscribe()
	s.serveStreamWebSocket(w, r, taskID, frames)
}

func (s *server) serveStreamWebSocket(
	w http.ResponseWriter,
	r *http.Request,
	taskID string,
	frames <-chan consoleStreamFrame,
) {
	if !s.webSockets.Begin() {
		writeError(w, http.StatusServiceUnavailable, "stream is shutting down")
		return
	}
	defer s.webSockets.Done()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return sameOriginRequest(r)
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	if !s.webSockets.Track(conn) {
		_ = conn.Close()
		return
	}
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
	defer func() {
		_ = conn.Close()
		<-readDone
		s.webSockets.Untrack(conn)
	}()
	if s.localRuntime != nil {
		logger := s.localRuntime.currentLogger()
		logger.Info("console_stream_ws_connected",
			"task_id", taskID,
			"remote_addr", strings.TrimSpace(r.RemoteAddr),
		)
		defer logger.Info("console_stream_ws_disconnected",
			"task_id", taskID,
			"remote_addr", strings.TrimSpace(r.RemoteAddr),
		)
	}

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

func relayRemoteStreamFrames(upstream *websocket.Conn, taskID string) (<-chan consoleStreamFrame, func()) {
	frames := make(chan consoleStreamFrame, 4)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(frames)
		for {
			var frame consoleStreamFrame
			if err := upstream.ReadJSON(&frame); err != nil {
				return
			}
			if strings.TrimSpace(frame.TaskID) != taskID {
				continue
			}
			select {
			case frames <- frame:
			case <-stop:
				return
			}
		}
	}()

	var stopOnce sync.Once
	return frames, func() {
		stopOnce.Do(func() {
			close(stop)
			_ = upstream.Close()
			<-done
		})
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
