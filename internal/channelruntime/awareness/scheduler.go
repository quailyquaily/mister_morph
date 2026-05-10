package awareness

import (
	"context"
	"time"

	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

type SchedulerOptions struct {
	InitialDelay     time.Duration
	Interval         time.Duration
	DisableHeartbeat bool
	PokeRequests     <-chan PokeRequest
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

	if !opts.DisableHeartbeat {
		if opts.InitialDelay > 0 {
			initialTimer := time.NewTimer(opts.InitialDelay)
			defer initialTimer.Stop()
			initialTriggered := false
			for !initialTriggered {
				select {
				case <-ctx.Done():
					return
				case req, ok := <-pokeRequests:
					if !ok {
						pokeRequests = nil
						continue
					}
					handlePoke(req)
				case <-initialTimer.C:
					runTick(awarenessutil.BehaviorHeartbeat, daemonruntime.PokeInput{})
					initialTriggered = true
				}
			}
		} else {
			runTick(awarenessutil.BehaviorHeartbeat, daemonruntime.PokeInput{})
		}
	}

	var ticker *time.Ticker
	var tickerC <-chan time.Time
	if !opts.DisableHeartbeat && opts.Interval > 0 {
		ticker = time.NewTicker(opts.Interval)
		tickerC = ticker.C
		defer ticker.Stop()
	}

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
		case <-tickerC:
			runTick(awarenessutil.BehaviorHeartbeat, daemonruntime.PokeInput{})
		}
	}
}
