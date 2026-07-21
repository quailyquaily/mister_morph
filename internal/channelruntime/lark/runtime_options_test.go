package lark

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

func TestNormalizeRunOptionsDefaults(t *testing.T) {
	t.Parallel()

	opts := normalizeRunOptions(RunOptions{})
	if opts.TaskTimeout != 10*time.Minute {
		t.Fatalf("task timeout = %s, want %s", opts.TaskTimeout, 10*time.Minute)
	}
	if opts.MaxConcurrency != 3 {
		t.Fatalf("max concurrency = %d, want 3", opts.MaxConcurrency)
	}
	if opts.ServerListen != "127.0.0.1:8790" {
		t.Fatalf("server listen = %q, want %q", opts.ServerListen, "127.0.0.1:8790")
	}
	if opts.FileCacheDir != pathutil.ExpandHomePath("~/.cache/morph") {
		t.Fatalf("file cache dir = %q, want %q", opts.FileCacheDir, pathutil.ExpandHomePath("~/.cache/morph"))
	}
}

func TestNormalizeRunOptionsDoesNotEnableServerWithInjectedTaskStore(t *testing.T) {
	t.Parallel()

	opts := normalizeRunOptions(RunOptions{TaskStore: daemonruntime.NewMemoryStore(10)})
	if opts.ServerListen != "" {
		t.Fatalf("server listen = %q, want disabled with injected task store", opts.ServerListen)
	}
}

func TestNormalizeRunOptionsPreservesFields(t *testing.T) {
	t.Parallel()

	opts := normalizeRunOptions(RunOptions{
		AppID:                         " app ",
		AppSecret:                     " secret ",
		AllowedChatIDs:                []string{" c1 ", "c1", "c2"},
		GroupTriggerMode:              " SMART ",
		AddressingConfidenceThreshold: 0.8,
		AddressingInterjectThreshold:  0.4,
		TaskTimeout:                   3 * time.Minute,
		MaxConcurrency:                7,
		FileCacheDir:                  " ~/.cache/custom ",
		ServerListen:                  " 127.0.0.1:9999 ",
		ServerAuthToken:               " auth ",
		ServerMaxQueue:                23,
		BaseURL:                       " https://example.com ",
		BusMaxInFlight:                55,
		RequestTimeout:                45 * time.Second,
		AgentLimits: agent.Limits{
			MaxSteps:        22,
			ParseRetries:    4,
			ToolRepeatLimit: 5,
		},
		MemoryEnabled:           true,
		MemoryShortTermDays:     9,
		MemoryInjectionEnabled:  true,
		MemoryInjectionMaxItems: 11,
		InspectPrompt:           true,
		InspectRequest:          true,
	})
	if opts.AppID != "app" || opts.AppSecret != "secret" {
		t.Fatalf("credentials were not normalized: %#v", opts)
	}
	if len(opts.AllowedChatIDs) != 2 || opts.AllowedChatIDs[0] != "c1" || opts.AllowedChatIDs[1] != "c2" {
		t.Fatalf("allowed chat ids = %#v, want [c1 c2]", opts.AllowedChatIDs)
	}
	if opts.ServerListen != "127.0.0.1:9999" || opts.ServerAuthToken != "auth" || opts.ServerMaxQueue != 23 {
		t.Fatalf("server options were not preserved: %#v", opts)
	}
	if opts.AgentLimits.MaxSteps != 22 || opts.AgentLimits.ParseRetries != 4 || opts.AgentLimits.ToolRepeatLimit != 5 {
		t.Fatalf("agent limits were not preserved: %#v", opts.AgentLimits)
	}
	if !opts.MemoryEnabled || !opts.MemoryInjectionEnabled || !opts.InspectPrompt || !opts.InspectRequest {
		t.Fatalf("boolean options were not preserved: %#v", opts)
	}
}
