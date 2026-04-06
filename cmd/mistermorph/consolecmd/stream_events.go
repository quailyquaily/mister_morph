package consolecmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

const consoleEventTailMaxChars = 6000
const consoleObserveTimeout = 8 * time.Second

type consoleObserveRequest struct {
	TaskID   string
	Profile  agent.ObserveProfile
	Trigger  string
	Snapshot string
}

type consoleSemanticObserver interface {
	Summarize(context.Context, consoleObserveRequest) (string, error)
}

type consoleEventPreviewSink struct {
	hub            *consoleStreamHub
	taskID         string
	logger         *slog.Logger
	now            func() time.Time
	observer       consoleSemanticObserver
	observeTimeout time.Duration
	observeCtx     context.Context
	observeCancel  context.CancelFunc
	observeWake    chan struct{}

	mu              sync.Mutex
	subtaskLine     string
	toolLine        string
	stdoutTail      string
	stderrTail      string
	observerSummary string
	subtaskProfile  agent.ObserveProfile
	toolProfile     agent.ObserveProfile
	pendingNewBytes int
	pendingEvents   int
	lastPublishAt   time.Time
	seenOutput      bool
	observeStarted  bool
	pendingObserve  *consoleObserveRequest
	observeCalls    map[agent.ObserveProfile]int
}

func newConsoleEventPreviewSink(hub *consoleStreamHub, taskID string, logger *slog.Logger) *consoleEventPreviewSink {
	observeCtx, observeCancel := context.WithCancel(context.Background())
	return &consoleEventPreviewSink{
		hub:            hub,
		taskID:         strings.TrimSpace(taskID),
		logger:         logger,
		now:            time.Now,
		observeTimeout: consoleObserveTimeout,
		observeCtx:     observeCtx,
		observeCancel:  observeCancel,
		observeWake:    make(chan struct{}, 1),
		observeCalls:   map[agent.ObserveProfile]int{},
	}
}

func (s *consoleEventPreviewSink) Close() {
	if s == nil || s.observeCancel == nil {
		return
	}
	s.observeCancel()
}

func (s *consoleEventPreviewSink) HandleEvent(_ context.Context, event agent.Event) {
	if s == nil || s.hub == nil || strings.TrimSpace(s.taskID) == "" {
		return
	}

	text, shouldPublish, observeReq := s.consume(event)
	if observeReq != nil {
		s.enqueueObserve(*observeReq)
	}
	if !shouldPublish || strings.TrimSpace(text) == "" {
		return
	}
	s.hub.PublishSnapshot(s.taskID, text)
}

func (s *consoleEventPreviewSink) consume(event agent.Event) (string, bool, *consoleObserveRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	activeProfileBefore := s.activeObserveProfileLocked()
	changed := false
	forcePublish := false
	switch strings.TrimSpace(event.Kind) {
	case agent.EventKindSubtaskStart:
		s.subtaskLine = formatConsoleSubtaskStart(event)
		s.subtaskProfile = agent.NormalizeObserveProfile(event.Profile)
		s.observerSummary = ""
		s.resetOutputTrackingLocked()
		changed = true
		forcePublish = true
	case agent.EventKindSubtaskDone:
		s.subtaskLine = formatConsoleSubtaskDone(event)
		s.subtaskProfile = agent.ObserveProfileDefault
		changed = true
		forcePublish = true
	case agent.EventKindToolStart:
		s.toolLine = formatConsoleToolStart(event)
		s.stdoutTail = ""
		s.stderrTail = ""
		s.observerSummary = ""
		s.toolProfile = profileForEvent(event, s.activeObserveProfileLocked())
		s.resetOutputTrackingLocked()
		changed = true
		forcePublish = true
	case agent.EventKindToolDone:
		s.toolLine = formatConsoleToolDone(event)
		s.toolProfile = agent.ObserveProfileDefault
		changed = true
		forcePublish = true
	case agent.EventKindToolOutput:
		switch strings.TrimSpace(event.Stream) {
		case "stdout":
			s.stdoutTail = appendConsoleTail(s.stdoutTail, event.Text, consoleEventTailMaxChars)
			changed = true
		case "stderr":
			s.stderrTail = appendConsoleTail(s.stderrTail, event.Text, consoleEventTailMaxChars)
			changed = true
		}
		if changed {
			s.pendingNewBytes += len(event.Text)
			s.pendingEvents++
			if !s.seenOutput && agent.ObservePolicyForProfile(s.activeObserveProfileLocked()).StreamOutput {
				s.seenOutput = true
				forcePublish = true
			} else if !s.seenOutput {
				s.seenOutput = true
			}
		}
	}

	if !changed {
		return "", false, nil
	}
	observeReq := s.buildObserveRequestLocked(event, activeProfileBefore)
	if !forcePublish && !s.shouldPublishLocked(now) {
		return "", false, observeReq
	}
	s.lastPublishAt = now
	s.pendingNewBytes = 0
	s.pendingEvents = 0
	text := s.renderLocked()
	if s.logger != nil && strings.TrimSpace(text) != "" {
		s.logger.Debug("console_event_preview_updated",
			"task_id", s.taskID,
			"kind", strings.TrimSpace(event.Kind),
			"tool", strings.TrimSpace(event.ToolName),
			"profile", string(s.activeObserveProfileLocked()),
			"chars", len(text),
		)
	}
	return text, true, observeReq
}

func (s *consoleEventPreviewSink) shouldPublishLocked(now time.Time) bool {
	policy := agent.ObservePolicyForProfile(s.activeObserveProfileLocked())
	if !policy.StreamOutput {
		return false
	}
	if policy.MinNewBytes > 0 && s.pendingNewBytes >= policy.MinNewBytes {
		return true
	}
	if policy.MinNewEvents > 0 && s.pendingEvents >= policy.MinNewEvents {
		return true
	}
	if policy.MinInterval > 0 && !s.lastPublishAt.IsZero() && now.Sub(s.lastPublishAt) >= policy.MinInterval {
		return true
	}
	return false
}

func (s *consoleEventPreviewSink) resetOutputTrackingLocked() {
	s.pendingNewBytes = 0
	s.pendingEvents = 0
	s.seenOutput = false
}

func (s *consoleEventPreviewSink) activeObserveProfileLocked() agent.ObserveProfile {
	if s.toolProfile != "" && s.toolProfile != agent.ObserveProfileDefault {
		return s.toolProfile
	}
	if s.subtaskProfile != "" && s.subtaskProfile != agent.ObserveProfileDefault {
		return s.subtaskProfile
	}
	return agent.ObserveProfileDefault
}

func (s *consoleEventPreviewSink) buildObserveRequestLocked(event agent.Event, activeProfileBefore agent.ObserveProfile) *consoleObserveRequest {
	if s.observer == nil {
		return nil
	}
	profile := profileForEvent(event, activeProfileBefore)
	policy := agent.ObservePolicyForProfile(profile)
	if policy.MaxLLMChecks <= 0 {
		return nil
	}
	if s.observeCalls[profile] >= policy.MaxLLMChecks {
		return nil
	}
	if !shouldObserveWithLLM(event, policy) {
		return nil
	}
	snapshot := s.renderObserveSnapshotLocked()
	if strings.TrimSpace(snapshot) == "" {
		return nil
	}
	s.observeCalls[profile] = s.observeCalls[profile] + 1
	return &consoleObserveRequest{
		TaskID:   s.taskID,
		Profile:  profile,
		Trigger:  strings.TrimSpace(event.Kind),
		Snapshot: snapshot,
	}
}

func (s *consoleEventPreviewSink) renderLocked() string {
	parts := make([]string, 0, 4)
	if line := strings.TrimSpace(s.subtaskLine); line != "" {
		parts = append(parts, line)
	}
	if line := strings.TrimSpace(s.toolLine); line != "" {
		parts = append(parts, line)
	}
	if summary := strings.TrimSpace(s.observerSummary); summary != "" {
		parts = append(parts, "summary:\n"+summary)
	}
	if out := strings.TrimSpace(s.stdoutTail); out != "" {
		parts = append(parts, "stdout:\n"+out)
	}
	if out := strings.TrimSpace(s.stderrTail); out != "" {
		parts = append(parts, "stderr:\n"+out)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (s *consoleEventPreviewSink) renderObserveSnapshotLocked() string {
	return s.renderLocked()
}

func formatConsoleSubtaskStart(event agent.Event) string {
	taskID := strings.TrimSpace(event.TaskID)
	mode := strings.TrimSpace(event.Mode)
	if mode == "" {
		mode = "agent"
	}
	if taskID == "" {
		return fmt.Sprintf("[subtask] started (%s)", mode)
	}
	return fmt.Sprintf("[subtask %s] started (%s)", taskID, mode)
}

func formatConsoleSubtaskDone(event agent.Event) string {
	taskID := strings.TrimSpace(event.TaskID)
	status := strings.TrimSpace(event.Status)
	if status == "" {
		status = "done"
	}
	base := "[subtask]"
	if taskID != "" {
		base = fmt.Sprintf("[subtask %s]", taskID)
	}
	if summary := strings.TrimSpace(event.Summary); summary != "" {
		return fmt.Sprintf("%s %s: %s", base, status, summary)
	}
	if errText := strings.TrimSpace(event.Error); errText != "" {
		return fmt.Sprintf("%s %s: %s", base, status, errText)
	}
	return fmt.Sprintf("%s %s", base, status)
}

func formatConsoleToolStart(event agent.Event) string {
	name := strings.TrimSpace(event.ToolName)
	if name == "" {
		name = "tool"
	}
	return fmt.Sprintf("[%s] running", name)
}

func formatConsoleToolDone(event agent.Event) string {
	name := strings.TrimSpace(event.ToolName)
	if name == "" {
		name = "tool"
	}
	if errText := strings.TrimSpace(event.Error); errText != "" {
		return fmt.Sprintf("[%s] failed: %s", name, daemonruntime.TruncateUTF8(errText, 160))
	}
	status := strings.TrimSpace(event.Status)
	if status == "" {
		status = "done"
	}
	return fmt.Sprintf("[%s] %s", name, status)
}

func profileForEvent(event agent.Event, fallback agent.ObserveProfile) agent.ObserveProfile {
	if profile := agent.NormalizeObserveProfile(event.Profile); profile != agent.ObserveProfileDefault {
		return profile
	}
	if strings.EqualFold(strings.TrimSpace(event.ToolName), "bash") {
		return agent.ObserveProfileLongShell
	}
	if fallback != "" {
		return fallback
	}
	return agent.ObserveProfileDefault
}

func shouldObserveWithLLM(event agent.Event, policy agent.ObservePolicy) bool {
	if policy.MaxLLMChecks <= 0 {
		return false
	}
	switch strings.TrimSpace(event.Kind) {
	case agent.EventKindToolDone:
		return true
	default:
		return false
	}
}

func (s *consoleEventPreviewSink) enqueueObserve(req consoleObserveRequest) {
	if s == nil || s.observer == nil {
		return
	}
	s.mu.Lock()
	s.pendingObserve = &req
	startWorker := !s.observeStarted
	if startWorker {
		s.observeStarted = true
	}
	s.mu.Unlock()

	if startWorker {
		go s.observeLoop()
	}
	select {
	case s.observeWake <- struct{}{}:
	default:
	}
}

func (s *consoleEventPreviewSink) observeLoop() {
	for {
		select {
		case <-s.observeCtx.Done():
			return
		case <-s.observeWake:
			req := s.takeObserveRequest()
			if req == nil {
				continue
			}
			summary, err := s.runObserve(*req)
			if err != nil {
				if s.logger != nil {
					s.logger.Warn("console_event_observer_failed",
						"task_id", s.taskID,
						"profile", string(req.Profile),
						"trigger", req.Trigger,
						"error", err.Error(),
					)
				}
				continue
			}
			if strings.TrimSpace(summary) == "" {
				continue
			}
			text := s.storeObservedSummary(summary)
			if strings.TrimSpace(text) == "" {
				continue
			}
			s.hub.PublishSnapshot(s.taskID, text)
		}
	}
}

func (s *consoleEventPreviewSink) takeObserveRequest() *consoleObserveRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	req := s.pendingObserve
	s.pendingObserve = nil
	return req
}

func (s *consoleEventPreviewSink) runObserve(req consoleObserveRequest) (string, error) {
	ctx := s.observeCtx
	cancel := func() {}
	if s.observeTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, s.observeTimeout)
	}
	defer cancel()
	return s.observer.Summarize(ctx, req)
}

func (s *consoleEventPreviewSink) storeObservedSummary(summary string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observerSummary = strings.TrimSpace(summary)
	return s.renderLocked()
}

func appendConsoleTail(existing string, chunk string, maxChars int) string {
	combined := existing + chunk
	if maxChars <= 0 {
		return combined
	}
	runes := []rune(combined)
	if len(runes) <= maxChars {
		return combined
	}
	keep := string(runes[len(runes)-maxChars:])
	return "...\n" + strings.TrimLeft(keep, "\n")
}
