package core

import (
	"strings"
	"sync"
	"time"
)

const (
	AgentInteractionLimit  = 8
	AgentInteractionWindow = 10 * time.Minute
)

type AgentInteractionLimiter struct {
	mu      sync.Mutex
	history map[string][]time.Time
}

func (l *AgentInteractionLimiter) Allow(conversationKey string, now time.Time) bool {
	conversationKey = strings.TrimSpace(conversationKey)
	if conversationKey == "" {
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.history == nil {
		l.history = make(map[string][]time.Time)
	}

	cutoff := now.Add(-AgentInteractionWindow)
	history := l.history[conversationKey]
	first := 0
	for first < len(history) && !history[first].After(cutoff) {
		first++
	}
	history = history[first:]
	if len(history) >= AgentInteractionLimit {
		l.history[conversationKey] = history
		return false
	}
	l.history[conversationKey] = append(history, now)
	return true
}
