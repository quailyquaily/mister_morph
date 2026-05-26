package awareness

import (
	"context"

	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func RunPokeLoop(ctx context.Context, pokeRequests <-chan PokeRequest, runPoke func(daemonruntime.PokeInput) awarenessutil.TickResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runPoke == nil {
		<-ctx.Done()
		return
	}

	handlePoke := func(req PokeRequest) {
		err := ErrorFromTickResult(runPoke(req.Input))
		if req.Result == nil {
			return
		}
		select {
		case req.Result <- err:
		default:
		}
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
		}
	}
}
