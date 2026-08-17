package core

import (
	"testing"
	"time"
)

func TestAgentInteractionLimiterUsesRollingWindowPerConversation(t *testing.T) {
	var limiter AgentInteractionLimiter
	start := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)

	for i := 0; i < AgentInteractionLimit; i++ {
		if !limiter.Allow("conversation-a", start.Add(time.Duration(i)*time.Minute)) {
			t.Fatalf("interaction %d rejected, want accepted", i+1)
		}
	}
	if limiter.Allow("conversation-a", start.Add(9*time.Minute)) {
		t.Fatal("ninth interaction within ten minutes accepted, want rejected")
	}
	if !limiter.Allow("conversation-b", start.Add(9*time.Minute)) {
		t.Fatal("first interaction in another conversation rejected")
	}
	if !limiter.Allow("conversation-a", start.Add(10*time.Minute)) {
		t.Fatal("interaction rejected after oldest entry left the rolling window")
	}
}

func TestAgentInteractionLimiterRejectsEmptyConversation(t *testing.T) {
	var limiter AgentInteractionLimiter
	if limiter.Allow("  ", time.Now()) {
		t.Fatal("empty conversation accepted")
	}
}
