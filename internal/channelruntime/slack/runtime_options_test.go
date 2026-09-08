package slack

import (
	"context"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

func TestNormalizeSlackRunStringSlice(t *testing.T) {
	got := normalizeRunStringSlice([]string{" T1 ", "", "T2", "T1", "  "})
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (%#v)", len(got), got)
	}
	if got[0] != "T1" || got[1] != "T2" {
		t.Fatalf("got = %#v, want [T1 T2]", got)
	}
}

func TestNormalizeRunOptionsDefaults(t *testing.T) {
	got := normalizeRunOptions(RunOptions{})
	if got.TaskTimeout != time.Hour {
		t.Fatalf("task timeout = %v, want 1h", got.TaskTimeout)
	}
	if got.BusMaxInFlight != 1024 {
		t.Fatalf("bus max inflight = %d, want 1024", got.BusMaxInFlight)
	}
	if got.RequestTimeout != 90*time.Second {
		t.Fatalf("request timeout = %v, want 90s", got.RequestTimeout)
	}
	if got.Server.Listen != "127.0.0.1:8788" {
		t.Fatalf("server listen = %q, want 127.0.0.1:8788", got.Server.Listen)
	}
	if got.AgentLimits.MaxSteps != 1024 {
		t.Fatalf("agent max steps = %d, want 1024", got.AgentLimits.MaxSteps)
	}
	if got.AgentLimits.ParseRetries != 16 {
		t.Fatalf("agent parse retries = %d, want 16", got.AgentLimits.ParseRetries)
	}
	if got.AgentLimits.ToolRepeatLimit != 256 {
		t.Fatalf("agent tool repeat limit = %d, want 256", got.AgentLimits.ToolRepeatLimit)
	}
	if got.MaxConcurrency != 3 {
		t.Fatalf("max concurrency = %d, want 3", got.MaxConcurrency)
	}
	if got.GroupTriggerMode != "smart" {
		t.Fatalf("group trigger mode = %q, want smart", got.GroupTriggerMode)
	}
	if got.BaseURL != "https://slack.com/api" {
		t.Fatalf("base url = %q, want https://slack.com/api", got.BaseURL)
	}
	if got.FileCacheDir != pathutil.ExpandHomePath("~/.cache/morph") {
		t.Fatalf("file cache dir = %q, want %q", got.FileCacheDir, pathutil.ExpandHomePath("~/.cache/morph"))
	}
	if got.AddressingConfidenceThreshold != 0.6 {
		t.Fatalf("confidence threshold = %v, want 0.6", got.AddressingConfidenceThreshold)
	}
	if got.AddressingInterjectThreshold != 0.6 {
		t.Fatalf("interject threshold = %v, want 0.6", got.AddressingInterjectThreshold)
	}
}

func TestNormalizeRunOptionsPreservesFields(t *testing.T) {
	got := normalizeRunOptions(RunOptions{
		BotToken:                      " xoxb ",
		AppToken:                      " xapp ",
		AllowedTeamIDs:                []string{" T1 ", "T1", "T2"},
		AllowedChannelIDs:             []string{" C1 ", "C1"},
		GroupTriggerMode:              "smart",
		AddressingConfidenceThreshold: 0.7,
		AddressingInterjectThreshold:  0.2,
		TaskTimeout:                   3 * time.Minute,
		MaxConcurrency:                7,
		FileCacheDir:                  " ~/.cache/custom ",
		Server: ServerOptions{
			Listen:  " 127.0.0.1:8080 ",
			CronRun: func(context.Context, cronstore.Task) error { return nil },
		},
		BaseURL:        " https://example.com/api ",
		BusMaxInFlight: 4096,
		RequestTimeout: 30 * time.Second,
		AgentLimits: agent.Limits{
			MaxSteps:        20,
			ParseRetries:    5,
			MaxTokenBudget:  2048,
			ToolRepeatLimit: 6,
		},
		InspectPrompt:  true,
		InspectRequest: true,
	})
	if got.BotToken != "xoxb" || got.AppToken != "xapp" {
		t.Fatalf("token normalization mismatch: %#v", got)
	}
	if len(got.AllowedTeamIDs) != 2 || got.AllowedTeamIDs[0] != "T1" || got.AllowedTeamIDs[1] != "T2" {
		t.Fatalf("allowed team ids = %#v, want [T1 T2]", got.AllowedTeamIDs)
	}
	if len(got.AllowedChannelIDs) != 1 || got.AllowedChannelIDs[0] != "C1" {
		t.Fatalf("allowed channel ids = %#v, want [C1]", got.AllowedChannelIDs)
	}
	if got.BaseURL != "https://example.com/api" || got.BusMaxInFlight != 4096 || got.AgentLimits.ParseRetries != 5 || got.AgentLimits.ToolRepeatLimit != 6 {
		t.Fatalf("resolved options mismatch: %#v", got)
	}
	if got.FileCacheDir != pathutil.ExpandHomePath("~/.cache/custom") {
		t.Fatalf("file cache dir = %q, want %q", got.FileCacheDir, pathutil.ExpandHomePath("~/.cache/custom"))
	}
	if got.Server.Listen != "127.0.0.1:8080" {
		t.Fatalf("server listen = %q, want 127.0.0.1:8080", got.Server.Listen)
	}
	if got.Server.CronRun == nil {
		t.Fatal("server CronRun = nil, want non-nil")
	}
	if !got.InspectPrompt || !got.InspectRequest {
		t.Fatalf("inspect options should be preserved: %#v", got)
	}
}
