package heartbeat

import (
	"context"
	"time"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
)

type SchedulerOptions struct {
	InitialDelay time.Duration
	Interval     time.Duration
	PokeRequests <-chan PokeRequest
}

func RunScheduler(ctx context.Context, opts SchedulerOptions, runTick func(daemonruntime.PokeInput) heartbeatutil.TickResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runTick == nil {
		<-ctx.Done()
		return
	}

	handlePoke := func(req PokeRequest) {
		err := ErrorFromTickResult(runTick(req.Input))
		if req.Result == nil {
			return
		}
		select {
		case req.Result <- err:
		default:
		}
	}

	pokeRequests := opts.PokeRequests

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
				initialTriggered = true
			case <-initialTimer.C:
				runTick(daemonruntime.PokeInput{})
				initialTriggered = true
			}
		}
	} else {
		runTick(daemonruntime.PokeInput{})
	}

	var ticker *time.Ticker
	var tickerC <-chan time.Time
	if opts.Interval > 0 {
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
			runTick(daemonruntime.PokeInput{})
		}
	}
}
