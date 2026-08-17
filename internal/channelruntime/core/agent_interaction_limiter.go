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
	mu          sync.Mutex
	history     map[string][]time.Time
	nextCleanup time.Time
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
	if l.nextCleanup.IsZero() || !now.Before(l.nextCleanup) {
		for key, history := range l.history {
			history = trimAgentInteractionHistory(history, cutoff)
			if len(history) == 0 {
				delete(l.history, key)
				continue
			}
			l.history[key] = history
		}
		l.nextCleanup = now.Add(AgentInteractionWindow)
	}
	history := trimAgentInteractionHistory(l.history[conversationKey], cutoff)
	if len(history) >= AgentInteractionLimit {
		l.history[conversationKey] = history
		return false
	}
	l.history[conversationKey] = append(history, now)
	return true
}

func trimAgentInteractionHistory(history []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(history) && !history[first].After(cutoff) {
		first++
	}
	return history[first:]
}
