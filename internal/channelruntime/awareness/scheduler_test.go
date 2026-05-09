package awareness

import (
	"context"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestRunSchedulerHandlesPokeBeforeInitialTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pokes := make(chan PokeRequest, 1)
	ticks := make(chan struct {
		behavior awarenessutil.Behavior
		input    daemonruntime.PokeInput
	}, 4)

	done := make(chan struct{})
	go func() {
		defer close(done)
		RunScheduler(ctx, SchedulerOptions{
			InitialDelay: 200 * time.Millisecond,
			Interval:     time.Hour,
			PokeRequests: pokes,
		}, func(behavior awarenessutil.Behavior, input daemonruntime.PokeInput) awarenessutil.TickResult {
			ticks <- struct {
				behavior awarenessutil.Behavior
				input    daemonruntime.PokeInput
			}{behavior: behavior, input: input}
			return awarenessutil.TickResult{Behavior: behavior, Outcome: awarenessutil.TickEnqueued}
		})
	}()

	req := PokeRequest{
		Input:  daemonruntime.PokeInput{HasBody: true, ContentType: "text/plain", BodyText: "test"},
		Result: make(chan error, 1),
	}
	pokes <- req

	select {
	case err := <-req.Result:
		if err != nil {
			t.Fatalf("poke result error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for poke result")
	}

	select {
	case got := <-ticks:
		if got.behavior != awarenessutil.BehaviorPoke {
			t.Fatalf("tick behavior = %q, want %q", got.behavior, awarenessutil.BehaviorPoke)
		}
		if got.input.BodyText != "test" {
			t.Fatalf("tick body = %q, want %q", got.input.BodyText, "test")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tick")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
