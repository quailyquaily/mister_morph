package awareness

import (
	"context"

	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

type SchedulerOptions struct {
	PokeRequests <-chan PokeRequest
}

func RunScheduler(ctx context.Context, opts SchedulerOptions, runTick func(awarenessutil.Behavior, daemonruntime.PokeInput) awarenessutil.TickResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runTick == nil {
		<-ctx.Done()
		return
	}

	handlePoke := func(req PokeRequest) {
		err := ErrorFromTickResult(runTick(awarenessutil.BehaviorPoke, req.Input))
		if req.Result == nil {
			return
		}
		select {
		case req.Result <- err:
		default:
		}
	}

	pokeRequests := opts.PokeRequests

	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-pokeRequests:
			if !ok {
				pokeRequests = nil
				continue
			}
			handlePoke(req)
		}
	}
}
