package mixin

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestNormalizeRunOptionsDefaults(t *testing.T) {
	t.Parallel()

	opts := normalizeRunOptions(RunOptions{})
	if opts.TaskTimeout != 10*time.Minute {
		t.Fatalf("task timeout = %s, want 10m", opts.TaskTimeout)
	}
	if opts.MaxConcurrency != 3 {
		t.Fatalf("max concurrency = %d, want 3", opts.MaxConcurrency)
	}
	if opts.ServerListen != "127.0.0.1:8792" {
		t.Fatalf("server listen = %q, want %q", opts.ServerListen, "127.0.0.1:8792")
	}
}

func TestMixinChannelOverviewIncludesConnectionState(t *testing.T) {
	t.Parallel()

	channel := mixinChannelOverview(true)
	if channel["configured"] != true || channel["mixin_running"] != true || channel["connected"] != true || channel["mixin_connected"] != true {
		t.Fatalf("mixinChannelOverview(true) = %#v", channel)
	}
	channel = mixinChannelOverview(false)
	if channel["connected"] != false || channel["mixin_connected"] != false {
		t.Fatalf("mixinChannelOverview(false) = %#v", channel)
	}
}

func TestNormalizeRunOptionsDoesNotEnableServerWithInjectedTaskStore(t *testing.T) {
	t.Parallel()

	opts := normalizeRunOptions(RunOptions{TaskStore: daemonruntime.NewMemoryStore(10)})
	if opts.ServerListen != "" {
		t.Fatalf("server listen = %q, want disabled", opts.ServerListen)
	}
}

func TestNormalizeRunOptionsPreservesAndDeduplicatesFields(t *testing.T) {
	t.Parallel()

	opts := normalizeRunOptions(RunOptions{
		KeystoreFile:           " ./mixin.json ",
		AllowedConversationIDs: []string{" c1 ", "c1", "c2"},
		TaskTimeout:            3 * time.Minute,
		MaxConcurrency:         7,
		ServerListen:           " 127.0.0.1:9999 ",
		ServerAuthToken:        " auth ",
		ServerMaxQueue:         23,
		BusMaxInFlight:         55,
		AgentLimits: agent.Limits{
			MaxSteps:        22,
			ParseRetries:    4,
			ToolRepeatLimit: 5,
		},
		InspectPrompt:  true,
		InspectRequest: true,
	})
	if opts.KeystoreFile != "mixin.json" {
		t.Fatalf("keystore file = %q", opts.KeystoreFile)
	}
	if len(opts.AllowedConversationIDs) != 2 || opts.AllowedConversationIDs[0] != "c1" || opts.AllowedConversationIDs[1] != "c2" {
		t.Fatalf("allowed conversations = %#v", opts.AllowedConversationIDs)
	}
	if opts.MaxConcurrency != 7 || opts.ServerAuthToken != "auth" {
		t.Fatalf("options were not preserved: %#v", opts)
	}
	if opts.AgentLimits.MaxSteps != 22 || !opts.InspectPrompt || !opts.InspectRequest {
		t.Fatalf("agent options were not preserved: %#v", opts)
	}
}
