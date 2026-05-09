package slackcmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
)

func TestBuildAwarenessRuntimePropagatesInspectFlags(t *testing.T) {
	_, hbOpts := buildAwarenessRuntime(
		Dependencies{},
		channelopts.SlackConfig{},
		channelopts.HeartbeatConfig{Interval: time.Minute},
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
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		defer server.Close()

		notifier := newSlackAwarenessNotifier("xoxb-test", server.URL, []string{"C111"})
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
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		defer server.Close()

		notifier := newSlackAwarenessNotifier("xoxb-test", server.URL, []string{" C111 ", "C111", "", "C222"})
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
