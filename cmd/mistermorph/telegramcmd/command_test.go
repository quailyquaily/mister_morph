package telegramcmd

import (
	"context"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelopts"
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	telegramruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/telegram"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
)

func TestBuildAwarenessRuntimePropagatesInspectFlags(t *testing.T) {
	_, hbOpts := buildAwarenessRuntime(
		Dependencies{},
		channelopts.TelegramConfig{},
		channelopts.HeartbeatConfig{Interval: time.Minute},
		channelopts.CronConfig{Enabled: true},
		"test-token",
		nil,
		2*time.Minute,
		toolsutil.RuntimeToolsRegisterConfig{},
		true,
		true,
	)
	if !hbOpts.InspectPrompt {
		t.Fatal("InspectPrompt = false, want true")
	}
	if !hbOpts.InspectRequest {
		t.Fatal("InspectRequest = false, want true")
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
