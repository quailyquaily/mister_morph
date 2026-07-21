package telegramcmd

import (
	"context"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	telegramruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/telegram"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

func TestBuildAwarenessRuntimePropagatesInspectFlags(t *testing.T) {
	base := dependencyCapabilitiesForTest()
	awarenessDeps, hbOpts := buildAwarenessRuntime(
		Dependencies{Dependencies: base},
		channelopts.TelegramConfig{},
		channelopts.HeartbeatConfig{Interval: time.Minute},
		channelopts.CronConfig{Enabled: true},
		"test-token",
		nil,
		2*time.Minute,
		toolsutil.RuntimeToolsRegisterConfig{},
		true,
		true,
		runtimepaths.Paths{},
		chatinfo.NewFetcher(chatinfo.FetcherOptions{TelegramBotToken: "snapshot-token"}),
	)
	if !hbOpts.InspectPrompt {
		t.Fatal("InspectPrompt = false, want true")
	}
	if !hbOpts.InspectRequest {
		t.Fatal("InspectRequest = false, want true")
	}
	if hbOpts.ChatInfoRefresher == nil {
		t.Fatal("ChatInfoRefresher = nil, want explicit snapshot dependency")
	}
	assertDependencyCapabilities(t, awarenessDeps)
}

func TestBuildTelegramRuntimeDepsPreservesCommonCapabilities(t *testing.T) {
	t.Parallel()

	base := dependencyCapabilitiesForTest()
	got := buildTelegramRuntimeDeps(Dependencies{Dependencies: base}, toolsutil.RuntimeToolsRegisterConfig{}, viper.New()).CommonDependencies
	assertDependencyCapabilities(t, got)
}

func dependencyCapabilitiesForTest() depsutil.CommonDependencies {
	return depsutil.CommonDependencies{
		ResolveLLMRouteWithProfile: func(string, string) (llmutil.ResolvedRoute, error) { return llmutil.ResolvedRoute{}, nil },
		AwarenessRegistry:          tools.NewRegistry,
		ToolTriggers:               func(string) map[string]bool { return map[string]bool{"sentinel": true} },
		RegisterTriggeredStaticTools: func(*tools.Registry, map[string]bool) {
		},
		ACPAgents: func() []acpclient.AgentConfig { return []acpclient.AgentConfig{{Name: "sentinel"}} },
	}
}

func assertDependencyCapabilities(t *testing.T, got depsutil.CommonDependencies) {
	t.Helper()
	if got.ResolveLLMRouteWithProfile == nil || got.AwarenessRegistry == nil || got.ToolTriggers == nil || got.RegisterTriggeredStaticTools == nil || got.ACPAgents == nil {
		t.Fatalf("common dependency capability was dropped: %#v", got)
	}
}

func TestAttachTelegramAwarenessTriggersProvidesCronRunner(t *testing.T) {
	var telegramOpts telegramruntime.RunOptions
	awarenessOpts := awarenessruntime.RunOptions{CronEnabled: true}
	attachTelegramAwarenessTriggers(&telegramOpts, &awarenessOpts)

	if telegramOpts.Server.Poke == nil {
		t.Fatal("Server.Poke = nil, want non-nil")
	}
	if telegramOpts.Server.CronRun == nil {
		t.Fatal("Server.CronRun = nil, want non-nil")
	}
	if awarenessOpts.PokeRequests == nil {
		t.Fatal("awareness PokeRequests = nil, want non-nil")
	}
	if awarenessOpts.CronRequests == nil {
		t.Fatal("awareness CronRequests = nil, want non-nil")
	}

	errCh := make(chan error, 1)
	task := cronstore.Task{
		ID:      "manual-task",
		Cron:    "0 10 * * *",
		Content: "Run manually.",
	}
	go func() {
		errCh <- telegramOpts.Server.CronRun(context.Background(), task)
	}()

	select {
	case req := <-awarenessOpts.CronRequests:
		if req.Task.ID != task.ID || req.Task.Content != task.Content {
			t.Fatalf("cron request task = %#v, want %#v", req.Task, task)
		}
		req.Result <- nil
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cron request")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("CronRun() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for CronRun result")
	}
}

func TestAttachTelegramAwarenessTriggersLeavesCronRunnerDisabledWhenCronDisabled(t *testing.T) {
	var telegramOpts telegramruntime.RunOptions
	var awarenessOpts awarenessruntime.RunOptions
	attachTelegramAwarenessTriggers(&telegramOpts, &awarenessOpts)

	if telegramOpts.Server.Poke == nil {
		t.Fatal("Server.Poke = nil, want non-nil")
	}
	if telegramOpts.Server.CronRun != nil {
		t.Fatal("Server.CronRun = non-nil, want nil when cron is disabled")
	}
	if awarenessOpts.CronRequests != nil {
		t.Fatal("awareness CronRequests = non-nil, want nil when cron is disabled")
	}
}
