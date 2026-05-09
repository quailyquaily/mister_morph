package awareness

import (
	"context"
	"errors"
	"fmt"

	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

type PokeRequest struct {
	Input  daemonruntime.PokeInput
	Result chan error
}

func Trigger(ctx context.Context, requests chan<- PokeRequest, input daemonruntime.PokeInput) error {
	if requests == nil {
		return fmt.Errorf("awareness poke is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	input = input.Normalize()
	if !input.HasBody || input.BodyText == "" {
		return awarenessutil.ErrEmptyPokeBody
	}
	req := PokeRequest{
		Input:  input,
		Result: make(chan error, 1),
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case requests <- req:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-req.Result:
		return err
	}
}

func ErrorFromTickResult(result awarenessutil.TickResult) error {
	switch result.Outcome {
	case awarenessutil.TickEnqueued:
		return nil
	case awarenessutil.TickBuildError:
		if errors.Is(result.BuildError, awarenessutil.ErrEmptyPokeBody) {
			return result.BuildError
		}
		if result.BuildError != nil {
			return result.BuildError
		}
		return fmt.Errorf("awareness poke failed")
	case awarenessutil.TickSkipped:
		switch result.SkipReason {
		case "", awarenessutil.SkipReasonEmptyTask:
			return nil
		case awarenessutil.SkipReasonAlreadyRunning:
			return daemonruntime.ErrPokeBusy
		default:
			return fmt.Errorf("awareness poke skipped: %s", result.SkipReason)
		}
	default:
		return fmt.Errorf("awareness poke failed")
	}
}
