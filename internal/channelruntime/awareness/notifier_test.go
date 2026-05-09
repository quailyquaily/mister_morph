package awareness

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestNotifyAwareness(t *testing.T) {
	t.Run("nil notifier", func(t *testing.T) {
		notifyAwareness(context.Background(), nil, nil, "hello")
	})

	t.Run("trim message before notify", func(t *testing.T) {
		var (
			called bool
			got    string
		)
		notifier := NotifyFunc(func(ctx context.Context, text string) error {
			_ = ctx
			called = true
			got = text
			return nil
		})
		notifyAwareness(context.Background(), notifier, nil, "  hello world  ")
		if !called {
			t.Fatalf("notifier was not called")
		}
		if got != "hello world" {
			t.Fatalf("notifier text = %q, want %q", got, "hello world")
		}
	})

	t.Run("alert messages are log only", func(t *testing.T) {
		called := false
		notifier := NotifyFunc(func(ctx context.Context, text string) error {
			_ = ctx
			_ = text
			called = true
			return nil
		})
		notifyAwareness(context.Background(), notifier, nil, "ALERT: awareness_failed (boom)")
		if called {
			t.Fatalf("notifier was called for alert message")
		}
	})

	t.Run("notifier error does not panic", func(t *testing.T) {
		notifier := NotifyFunc(func(ctx context.Context, text string) error {
			_ = ctx
			_ = text
			return errors.New("boom")
		})
		notifyAwareness(context.Background(), notifier, slog.Default(), "ping")
	})
}
