package llmutil

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

const llmRequestRetries = 5

var errStreamConsumer = errors.New("llm stream consumer failed")

func (c *fallbackClient) chatWithRetry(ctx context.Context, client llm.Client, req llm.Request, profile string) (llm.Result, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return llm.Result{}, err
		}
		// Each provider call creates a fresh request timeout under the task context.
		attemptReq := req
		streamOpen := false
		var streamErr error
		if req.OnStream != nil {
			attemptReq.OnStream = func(event llm.StreamEvent) error {
				streamOpen = !event.Done
				if err := req.OnStream(event); err != nil {
					streamErr = err
					return err
				}
				return nil
			}
		}
		result, err := client.Chat(ctx, attemptReq)
		if streamErr != nil {
			return llm.Result{}, errors.Join(errStreamConsumer, streamErr)
		}
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return llm.Result{}, ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return llm.Result{}, err
		}
		// End a partial stream before another attempt or fallback starts. Done is
		// the per-request boundary used by streaming parsers, not task completion.
		if streamOpen {
			if streamErr := req.OnStream(llm.StreamEvent{Done: true}); streamErr != nil {
				return llm.Result{}, errors.Join(errStreamConsumer, streamErr)
			}
		}
		reason, eligible := fallbackEligibleReason(err)
		if attempt >= llmRequestRetries || !eligible {
			return llm.Result{}, err
		}
		// Preserve direct fallback for authentication, model/media compatibility,
		// and rate-limit errors. Retry all other request failures on this profile.
		switch reason {
		case "status_401", "status_403", "status_404", "status_415", "status_422", "status_429":
			return llm.Result{}, err
		}

		// Equal jitter: wait between half and all of 1, 2, 4, 8, then 16 seconds.
		limit := time.Second << attempt
		delay := limit/2 + time.Duration(rand.Int64N(int64(limit/2)))
		if c.logger != nil {
			c.logger.Warn("llm_request_retry",
				"profile", profile, "model", req.Model, "scene", req.Scene,
				"retry", attempt+1, "max_retries", llmRequestRetries, "reason", reason,
				"delay", delay, "error", err.Error())
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return llm.Result{}, ctx.Err()
		case <-timer.C:
		}
	}
}
