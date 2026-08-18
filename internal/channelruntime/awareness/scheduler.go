package awareness

import (
	"context"

	awarenessdomain "github.com/quailyquaily/mistermorph/internal/awareness"
	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
)

func RunPokeLoop(ctx context.Context, pokeRequests <-chan PokeRequest, runPoke func(awarenessdomain.PokeInput) awarenessutil.TickResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	if runPoke == nil {
		<-ctx.Done()
		return
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
			err := ErrorFromTickResult(runPoke(req.Input))
			if req.Result != nil {
				select {
				case req.Result <- err:
				default:
				}
			}
		}
	}
}
