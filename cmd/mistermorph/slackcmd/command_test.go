package slackcmd

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelopts"
	awarenessruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	slackruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/slack"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/testhttp"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
)

func TestBuildAwarenessRuntimePropagatesInspectFlags(t *testing.T) {
	_, hbOpts := buildAwarenessRuntime(
		Dependencies{},
		channelopts.SlackConfig{},
		channelopts.HeartbeatConfig{Interval: time.Minute},
		channelopts.CronConfig{Enabled: true},
		"xoxb-test",
		nil,
		2*time.Minute,
		"",
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

func TestAttachSlackAwarenessTriggersProvidesCronRunner(t *testing.T) {
	var slackOpts slackruntime.RunOptions
	awarenessOpts := awarenessruntime.RunOptions{CronEnabled: true}
	attachSlackAwarenessTriggers(&slackOpts, &awarenessOpts)

	if slackOpts.Server.Poke == nil {
		t.Fatal("Server.Poke = nil, want non-nil")
	}
	if slackOpts.Server.CronRun == nil {
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
		errCh <- slackOpts.Server.CronRun(context.Background(), task)
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

func TestAttachSlackAwarenessTriggersLeavesCronRunnerDisabledWhenCronDisabled(t *testing.T) {
	var slackOpts slackruntime.RunOptions
	var awarenessOpts awarenessruntime.RunOptions
	attachSlackAwarenessTriggers(&slackOpts, &awarenessOpts)

	if slackOpts.Server.Poke == nil {
		t.Fatal("Server.Poke = nil, want non-nil")
	}
	if slackOpts.Server.CronRun != nil {
		t.Fatal("Server.CronRun = non-nil, want nil when cron is disabled")
	}
	if awarenessOpts.CronRequests != nil {
		t.Fatal("awareness CronRequests = non-nil, want nil when cron is disabled")
	}
}

func TestNewSlackAwarenessNotifier(t *testing.T) {
	t.Run("nil when no channel ids", func(t *testing.T) {
		if got := newSlackAwarenessNotifier("xoxb-test", "", nil); got != nil {
			t.Fatalf("notifier = %#v, want nil", got)
		}
		if got := newSlackAwarenessNotifier("xoxb-test", "", []string{"", "   "}); got != nil {
			t.Fatalf("notifier = %#v, want nil", got)
		}
	})

	t.Run("empty text does not send", func(t *testing.T) {
		var callCount int
		serverURL := testhttp.WithDefaultTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))

		notifier := newSlackAwarenessNotifier("xoxb-test", serverURL, []string{"C111"})
		if notifier == nil {
			t.Fatalf("notifier = nil, want non-nil")
		}
		if err := notifier.Notify(context.Background(), "   "); err != nil {
			t.Fatalf("Notify() error = %v", err)
		}
		if callCount != 0 {
			t.Fatalf("call count = %d, want 0", callCount)
		}
	})

	t.Run("send deduped channels", func(t *testing.T) {
		var (
			mu       sync.Mutex
			channels []string
			texts    []string
		)
		serverURL := testhttp.WithDefaultTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat.postMessage" {
				t.Fatalf("path = %q, want %q", r.URL.Path, "/chat.postMessage")
			}
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer xoxb-test" {
				t.Fatalf("authorization = %q", got)
			}
			var payload struct {
				Channel string `json:"channel"`
				Text    string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			mu.Lock()
			channels = append(channels, strings.TrimSpace(payload.Channel))
			texts = append(texts, strings.TrimSpace(payload.Text))
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))

		notifier := newSlackAwarenessNotifier("xoxb-test", serverURL, []string{" C111 ", "C111", "", "C222"})
		if notifier == nil {
			t.Fatalf("notifier = nil, want non-nil")
		}
		if err := notifier.Notify(context.Background(), "heartbeat: ping"); err != nil {
			t.Fatalf("Notify() error = %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(channels) != 2 {
			t.Fatalf("channels len = %d, want 2", len(channels))
		}
		if channels[0] != "C111" || channels[1] != "C222" {
			t.Fatalf("channels = %#v, want [C111 C222]", channels)
		}
		if texts[0] != "heartbeat: ping" || texts[1] != "heartbeat: ping" {
			t.Fatalf("texts = %#v, want both heartbeat: ping", texts)
		}
	})
}
