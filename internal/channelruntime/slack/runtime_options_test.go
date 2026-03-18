package slack

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
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

func TestNormalizeRuntimeLoopOptionsDefaults(t *testing.T) {
	got := normalizeRuntimeLoopOptions(runtimeLoopOptions{})
	if got.TaskTimeout != 10*time.Minute {
		t.Fatalf("task timeout = %v, want 10m", got.TaskTimeout)
	}
	if got.BusMaxInFlight != 1024 {
		t.Fatalf("bus max inflight = %d, want 1024", got.BusMaxInFlight)
	}
	if got.RequestTimeout != 90*time.Second {
		t.Fatalf("request timeout = %v, want 90s", got.RequestTimeout)
	}
	if got.MemoryShortTermDays != 7 {
		t.Fatalf("memory short term days = %d, want 7", got.MemoryShortTermDays)
	}
	if got.MemoryInjectionMaxItems != 50 {
		t.Fatalf("memory injection max items = %d, want 50", got.MemoryInjectionMaxItems)
	}
	if got.Server.Listen != "127.0.0.1:8787" {
		t.Fatalf("server listen = %q, want 127.0.0.1:8787", got.Server.Listen)
	}
	if got.AgentLimits.MaxSteps != 15 {
		t.Fatalf("agent max steps = %d, want 15", got.AgentLimits.MaxSteps)
	}
	if got.AgentLimits.ParseRetries != 2 {
		t.Fatalf("agent parse retries = %d, want 2", got.AgentLimits.ParseRetries)
	}
	if got.AgentLimits.ToolRepeatLimit != 3 {
		t.Fatalf("agent tool repeat limit = %d, want 3", got.AgentLimits.ToolRepeatLimit)
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

func TestResolveRuntimeLoopOptionsFromRunOptions(t *testing.T) {
	got := resolveRuntimeLoopOptionsFromRunOptions(RunOptions{
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
			Listen: " 127.0.0.1:8080 ",
		},
		BaseURL:                 " https://example.com/api ",
		BusMaxInFlight:          4096,
		RequestTimeout:          30 * time.Second,
		MemoryEnabled:           true,
		MemoryShortTermDays:     9,
		MemoryInjectionEnabled:  true,
		MemoryInjectionMaxItems: 12,
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
	if !got.MemoryEnabled || got.MemoryShortTermDays != 9 || !got.MemoryInjectionEnabled || got.MemoryInjectionMaxItems != 12 {
		t.Fatalf("memory options mismatch: %#v", got)
	}
	if !got.InspectPrompt || !got.InspectRequest {
		t.Fatalf("inspect options should be preserved: %#v", got)
	}
}
