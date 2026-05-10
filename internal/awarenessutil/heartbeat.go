package awarenessutil

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

const (
	awarenessFailureThreshold = 3
)

var awarenessHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)

type Behavior string

const (
	BehaviorHeartbeat Behavior = "heartbeat"
	BehaviorPoke      Behavior = "poke"
)

var ErrEmptyPokeBody = errors.New("poke body text is required")

func NormalizeBehavior(raw string) Behavior {
	switch Behavior(strings.ToLower(strings.TrimSpace(raw))) {
	case BehaviorPoke:
		return BehaviorPoke
	default:
		return BehaviorHeartbeat
	}
}

func BuildAwarenessTask(behavior Behavior, checklistPath string, input daemonruntime.PokeInput) (string, bool, error) {
	switch NormalizeBehavior(string(behavior)) {
	case BehaviorPoke:
		return BuildPokeTask(input)
	default:
		return BuildHeartbeatTask(checklistPath)
	}
}

func BuildHeartbeatTask(checklistPath string) (string, bool, error) {
	return readHeartbeatChecklist(checklistPath)
}

func BuildPokeTask(input daemonruntime.PokeInput) (string, bool, error) {
	input = input.Normalize()
	if !input.HasBody || strings.TrimSpace(input.BodyText) == "" {
		return "", true, ErrEmptyPokeBody
	}
	return strings.TrimSpace(input.BodyText), false, nil
}

func BuildAwarenessMeta(behavior Behavior, source string, interval time.Duration, checklistPath string, taskEmpty bool, input daemonruntime.PokeInput, extra map[string]any) map[string]any {
	behavior = NormalizeBehavior(string(behavior))
	awareness := map[string]any{
		"behavior":         string(behavior),
		"source":           source,
		"scheduled_at_utc": time.Now().UTC().Format(time.RFC3339),
	}
	if behavior == BehaviorHeartbeat && interval > 0 {
		awareness["interval"] = interval.String()
	}
	if behavior == BehaviorHeartbeat && strings.TrimSpace(checklistPath) != "" {
		awareness["checklist_path"] = checklistPath
	}
	if taskEmpty {
		awareness["task_empty"] = true
	}
	if pokeMeta := input.MetaValue(); pokeMeta != nil {
		awareness["poke"] = pokeMeta
	}
	for k, v := range extra {
		if strings.TrimSpace(k) == "" {
			continue
		}
		awareness[k] = v
	}
	out := map[string]any{
		"trigger":   string(behavior),
		"awareness": awareness,
	}
	if behavior == BehaviorHeartbeat {
		out["heartbeat"] = awareness
	}
	return out
}

type State struct {
	mu          sync.Mutex
	running     bool
	failures    int
	lastSuccess time.Time
	lastError   string
}

func (s *State) Start() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *State) EndSkipped() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

func (s *State) EndSuccess(now time.Time) {
	s.mu.Lock()
	s.running = false
	s.failures = 0
	s.lastError = ""
	s.lastSuccess = now
	s.mu.Unlock()
}

func (s *State) EndFailure(err error) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.failures++
	if err != nil {
		s.lastError = strings.TrimSpace(err.Error())
	}
	if s.failures >= awarenessFailureThreshold {
		msg := "awareness_failed"
		if s.lastError != "" {
			msg = fmt.Sprintf("awareness_failed (%s)", s.lastError)
		}
		s.failures = 0
		return true, "ALERT: " + msg
	}
	return false, ""
}

func (s *State) Snapshot() (failures int, lastSuccess time.Time, lastError string, running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failures, s.lastSuccess, s.lastError, s.running
}

func readHeartbeatChecklist(path string) (string, bool, error) {
	path = strings.TrimSpace(path)
	if strings.TrimSpace(path) == "" {
		return "", true, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", true, nil
		}
		return "", true, err
	}
	content := string(raw)
	if isChecklistEmptyContent(content) {
		return "", true, nil
	}
	return strings.TrimSpace(content), false, nil
}

func isChecklistEmptyContent(content string) bool {
	stripped := awarenessHTMLComment.ReplaceAllString(content, "")
	lines := strings.Split(stripped, "\n")
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "#") {
			continue
		}
		return false
	}
	return true
}
