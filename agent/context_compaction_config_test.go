package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

func TestRunRejectsInvalidContextCompactionConfig(t *testing.T) {
	client := newMockClient(finalResponse("unexpected"))
	engine := New(client, tools.NewRegistry(), Config{
		ContextCompaction: ContextCompactionConfig{TriggerRatio: 1},
	}, DefaultPromptSpec())

	_, _, err := engine.Run(context.Background(), "task", RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "context compaction trigger ratio") {
		t.Fatalf("Run() error = %v", err)
	}
	if calls := client.allCalls(); len(calls) != 0 {
		t.Fatalf("client calls = %d, want 0", len(calls))
	}
}

func TestNewContextCompactionConfigKeepsExplicitDisabledValue(t *testing.T) {
	config := NewContextCompactionConfig(false, 0.75)
	resolved := resolveContextCompactionConfig(config, false)
	if resolved.Enabled {
		t.Fatal("resolved enabled = true, want false")
	}
	if resolved.TriggerRatio != 0.75 {
		t.Fatalf("resolved config = %+v", resolved)
	}
}

func TestNewContextCompactionConfigRejectsExplicitZeroTriggerRatio(t *testing.T) {
	config := NewContextCompactionConfig(true, 0)
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "trigger ratio") {
		t.Fatalf("Validate() error = %v, want trigger ratio error", err)
	}
}
