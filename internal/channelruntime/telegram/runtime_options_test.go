package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
)

func TestNormalizeRunOptionsPreservesFields(t *testing.T) {
	got := normalizeRunOptions(RunOptions{
		BotToken:                      " token ",
		AllowedChatIDs:                []int64{1, 1, 2},
		GroupTriggerMode:              "smart",
		AddressingConfidenceThreshold: 0.7,
		AddressingInterjectThreshold:  0.3,
		PollTimeout:                   45 * time.Second,
		TaskTimeout:                   2 * time.Minute,
		MaxConcurrency:                5,
		FileCacheDir:                  " /tmp/cache ",
		Server: ServerOptions{
			Listen:  "127.0.0.1:8080",
			CronRun: func(context.Context, cronstore.Task) error { return nil },
		},
		BusMaxInFlight: 2048,
		RequestTimeout: 75 * time.Second,
		AgentLimits: agent.Limits{
			MaxSteps:        20,
			ParseRetries:    4,
			MaxTokenBudget:  1000,
			ToolRepeatLimit: 6,
		},
		FileCacheMaxAge:        24 * time.Hour,
		FileCacheMaxFiles:      200,
		FileCacheMaxTotalBytes: int64(64 * 1024 * 1024),
		InspectPrompt:          true,
		InspectRequest:         true,
	})
	if got.BotToken != "token" {
		t.Fatalf("bot token = %q, want token", got.BotToken)
	}
	if len(got.AllowedChatIDs) != 2 || got.AllowedChatIDs[0] != 1 || got.AllowedChatIDs[1] != 2 {
		t.Fatalf("allowed chat ids = %#v, want [1 2]", got.AllowedChatIDs)
	}
	if got.BusMaxInFlight != 2048 || got.AgentLimits.MaxSteps != 20 || got.AgentLimits.ToolRepeatLimit != 6 || got.FileCacheMaxFiles != 200 {
		t.Fatalf("resolved options mismatch: %#v", got)
	}
	if got.Server.Listen != "127.0.0.1:8080" {
		t.Fatalf("server listen = %q, want 127.0.0.1:8080", got.Server.Listen)
	}
	if got.Server.CronRun == nil {
		t.Fatal("server CronRun = nil, want non-nil")
	}
	if !got.InspectPrompt || !got.InspectRequest {
		t.Fatalf("boolean run options should be preserved: %#v", got)
	}
}

func TestNormalizeRunOptionsDefaults(t *testing.T) {
	got := normalizeRunOptions(RunOptions{})
	if got.PollTimeout != 30*time.Second {
		t.Fatalf("poll timeout = %v, want 30s", got.PollTimeout)
	}
	if got.TaskTimeout != 10*time.Minute {
		t.Fatalf("task timeout = %v, want 10m", got.TaskTimeout)
	}
	if got.MaxConcurrency != 3 {
		t.Fatalf("max concurrency = %d, want 3", got.MaxConcurrency)
	}
	if got.BusMaxInFlight != 1024 {
		t.Fatalf("bus max inflight = %d, want 1024", got.BusMaxInFlight)
	}
	if got.RequestTimeout != 90*time.Second {
		t.Fatalf("request timeout = %v, want 90s", got.RequestTimeout)
	}
	if got.AgentLimits.MaxSteps != 15 {
		t.Fatalf("agent max steps = %d, want 15", got.AgentLimits.MaxSteps)
	}
	if got.AgentLimits.ParseRetries != 2 {
		t.Fatalf("agent parse retries = %d, want 2", got.AgentLimits.ParseRetries)
	}
	if got.AgentLimits.ToolRepeatLimit != 64 {
		t.Fatalf("agent tool repeat limit = %d, want 64", got.AgentLimits.ToolRepeatLimit)
	}
	if got.FileCacheDir != "~/.cache/morph" {
		t.Fatalf("file cache dir = %q, want ~/.cache/morph", got.FileCacheDir)
	}
	if got.FileCacheMaxAge != 7*24*time.Hour {
		t.Fatalf("file cache max age = %v, want 168h", got.FileCacheMaxAge)
	}
	if got.FileCacheMaxFiles != 1000 {
		t.Fatalf("file cache max files = %d, want 1000", got.FileCacheMaxFiles)
	}
	if got.FileCacheMaxTotalBytes != int64(512*1024*1024) {
		t.Fatalf("file cache max total bytes = %d, want 536870912", got.FileCacheMaxTotalBytes)
	}
	if got.Server.Listen != "127.0.0.1:8787" {
		t.Fatalf("server listen = %q, want 127.0.0.1:8787", got.Server.Listen)
	}
	if got.GroupTriggerMode != "smart" {
		t.Fatalf("group trigger mode = %q, want smart", got.GroupTriggerMode)
	}
	if got.AddressingConfidenceThreshold != 0.6 {
		t.Fatalf("confidence threshold = %v, want 0.6", got.AddressingConfidenceThreshold)
	}
	if got.AddressingInterjectThreshold != 0.6 {
		t.Fatalf("interject threshold = %v, want 0.6", got.AddressingInterjectThreshold)
	}
}
