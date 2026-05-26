package awareness

import (
	"context"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestRunSchedulerHandlesPoke(t *testing.T) {
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

	select {
	case got := <-ticks:
		t.Fatalf("unexpected tick behavior = %q", got.behavior)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func TestRunSchedulerDoesNotEmitHeartbeatTicks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pokes := make(chan PokeRequest, 1)
	ticks := make(chan awarenessutil.Behavior, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunScheduler(ctx, SchedulerOptions{
			PokeRequests: pokes,
		}, func(behavior awarenessutil.Behavior, input daemonruntime.PokeInput) awarenessutil.TickResult {
			ticks <- behavior
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
		if got != awarenessutil.BehaviorPoke {
			t.Fatalf("tick behavior = %q, want %q", got, awarenessutil.BehaviorPoke)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for poke tick")
	}

	select {
	case got := <-ticks:
		t.Fatalf("unexpected tick behavior = %q", got)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
