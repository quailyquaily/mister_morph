package telegramcmd

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
)

func TestBuildAwarenessRuntimePropagatesInspectFlags(t *testing.T) {
	_, hbOpts := buildAwarenessRuntime(
		Dependencies{},
		channelopts.TelegramConfig{},
		channelopts.HeartbeatConfig{Interval: time.Minute},
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
