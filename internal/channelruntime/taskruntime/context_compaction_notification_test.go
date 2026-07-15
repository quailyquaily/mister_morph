package taskruntime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
)

func TestWithContextCompactionNotificationChainsAndIsolatesFailure(t *testing.T) {
	var forwarded []agent.Event
	base := agent.WithEventSinkContext(context.Background(), agent.EventSinkFunc(func(_ context.Context, event agent.Event) {
		forwarded = append(forwarded, event)
	}))
	var notified []string
	ctx := WithContextCompactionNotification(base, slog.New(slog.NewTextHandler(io.Discard, nil)), func(_ context.Context, event agent.Event, text string) error {
		if event.Kind != agent.EventKindContextCompactionDone {
			t.Fatalf("notification event kind = %q", event.Kind)
		}
		notified = append(notified, text)
		return errors.New("send failed")
	})

	agent.EmitEvent(ctx, nil, agent.Event{Kind: agent.EventKindContextCompactionStart})
	agent.EmitEvent(ctx, nil, agent.Event{Kind: agent.EventKindContextCompactionDone})

	if len(forwarded) != 2 {
		t.Fatalf("forwarded events = %d, want 2", len(forwarded))
	}
	if len(notified) != 1 || notified[0] != ContextCompactionDoneText {
		t.Fatalf("notifications = %#v", notified)
	}
}
