package line

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
)

func TestNormalizeRunOptionsDefaults(t *testing.T) {
	t.Parallel()

	opts := normalizeRunOptions(RunOptions{})
	if opts.TaskTimeout != time.Hour {
		t.Fatalf("task timeout = %s, want %s", opts.TaskTimeout, time.Hour)
	}
	if opts.MaxConcurrency != 3 {
		t.Fatalf("max concurrency = %d, want 3", opts.MaxConcurrency)
	}
	if opts.WebhookListen != "127.0.0.1:18080" {
		t.Fatalf("webhook listen = %q, want %q", opts.WebhookListen, "127.0.0.1:18080")
	}
	if opts.WebhookPath != "/line/webhook" {
		t.Fatalf("webhook path = %q, want %q", opts.WebhookPath, "/line/webhook")
	}
	if opts.FileCacheDir != "~/.cache/morph" {
		t.Fatalf("file cache dir = %q, want %q", opts.FileCacheDir, "~/.cache/morph")
	}
	if opts.ServerListen != "127.0.0.1:8789" {
		t.Fatalf("server listen = %q, want %q", opts.ServerListen, "127.0.0.1:8789")
	}
}

func TestNormalizeRunOptionsWebhookPath(t *testing.T) {
	t.Parallel()

	opts := normalizeRunOptions(RunOptions{
		WebhookPath: "line/hook",
	})
	if opts.WebhookPath != "/line/hook" {
		t.Fatalf("webhook path = %q, want %q", opts.WebhookPath, "/line/hook")
	}
}

func TestNormalizeRunOptionsPreservesFields(t *testing.T) {
	t.Parallel()

	opts := normalizeRunOptions(RunOptions{
		ChannelAccessToken:            " token ",
		ChannelSecret:                 " secret ",
		AllowedGroupIDs:               []string{" g1 ", "g1", "g2"},
		GroupTriggerMode:              " SMART ",
		AddressingConfidenceThreshold: 0.8,
		AddressingInterjectThreshold:  0.4,
		TaskTimeout:                   3 * time.Minute,
		MaxConcurrency:                7,
		FileCacheDir:                  " /cache ",
		ServerListen:                  " 127.0.0.1:9999 ",
		ServerAuthToken:               " auth ",
		ServerMaxQueue:                23,
		BaseURL:                       " https://example.com ",
		WebhookListen:                 " 127.0.0.1:18888 ",
		WebhookPath:                   " hook ",
		BusMaxInFlight:                55,
		RequestTimeout:                45 * time.Second,
		AgentLimits: agent.Limits{
			MaxSteps:        22,
			ParseRetries:    4,
			ToolRepeatLimit: 5,
		},
		InspectPrompt:  true,
		InspectRequest: true,
	})
	if opts.ChannelAccessToken != "token" || opts.ChannelSecret != "secret" {
		t.Fatalf("credentials were not normalized: %#v", opts)
	}
	if len(opts.AllowedGroupIDs) != 2 || opts.AllowedGroupIDs[0] != "g1" || opts.AllowedGroupIDs[1] != "g2" {
		t.Fatalf("allowed group ids = %#v, want [g1 g2]", opts.AllowedGroupIDs)
	}
	if opts.ServerListen != "127.0.0.1:9999" || opts.ServerAuthToken != "auth" || opts.ServerMaxQueue != 23 {
		t.Fatalf("server options were not preserved: %#v", opts)
	}
	if opts.WebhookListen != "127.0.0.1:18888" || opts.WebhookPath != "/hook" {
		t.Fatalf("webhook options were not normalized: %#v", opts)
	}
	if opts.AgentLimits.MaxSteps != 22 || opts.AgentLimits.ParseRetries != 4 || opts.AgentLimits.ToolRepeatLimit != 5 {
		t.Fatalf("agent limits were not preserved: %#v", opts.AgentLimits)
	}
	if !opts.InspectPrompt || !opts.InspectRequest {
		t.Fatalf("boolean options were not preserved: %#v", opts)
	}
}

func TestNormalizeRunStringSlice(t *testing.T) {
	t.Parallel()

	got := normalizeRunStringSlice([]string{" G1 ", "", "G2", "G1"})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0] != "G1" || got[1] != "G2" {
		t.Fatalf("values = %#v, want [G1 G2]", got)
	}
}
