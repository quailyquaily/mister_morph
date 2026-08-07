package memoryruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/quailyquaily/mistermorph/memory"
)

const (
	defaultProjectionInterval           = 10 * time.Minute
	defaultProjectionNewRecordThreshold = 10
	defaultProjectionLimit              = 50
	defaultProjectionMaxRounds          = 20
)

type ProjectionWorkerOptions struct {
	// Interval is the timer trigger period. Each tick attempts one projection run.
	Interval time.Duration
	// NewRecordThreshold is the count-trigger threshold. For count-trigger runs,
	// projection starts only when the journal has at least this many unapplied records.
	NewRecordThreshold int
	// ProjectLimit is the maximum journal records consumed by one ProjectOnce call.
	ProjectLimit int
	// MaxRounds is the maximum ProjectOnce rounds executed in one trigger cycle.
	// This bounds backlog drain work per trigger.
	MaxRounds int
	// Logger receives worker run/projection errors. Nil disables worker logs.
	Logger *slog.Logger
}

type projectionTrigger string

const (
	projectionTriggerTimer projectionTrigger = "timer"
	projectionTriggerCount projectionTrigger = "count"
)

type ProjectionWorker struct {
	journal   memory.EventJournal
	projector *memory.Projector
	opts      ProjectionWorkerOptions
	wakeCh    chan struct{}
	running   atomic.Bool
}

func NewProjectionWorker(journal memory.EventJournal, projector *memory.Projector, opts ProjectionWorkerOptions) (*ProjectionWorker, error) {
	if journal == nil {
		return nil, fmt.Errorf("memory journal is required")
	}
	if projector == nil {
		return nil, fmt.Errorf("memory projector is required")
	}
	opts = normalizeProjectionWorkerOptions(opts)
	return &ProjectionWorker{
		journal:   journal,
		projector: projector,
		opts:      opts,
		wakeCh:    make(chan struct{}, 1),
	}, nil
}

func normalizeProjectionWorkerOptions(opts ProjectionWorkerOptions) ProjectionWorkerOptions {
	if opts.Interval <= 0 {
		opts.Interval = defaultProjectionInterval
	}
	if opts.NewRecordThreshold <= 0 {
		opts.NewRecordThreshold = defaultProjectionNewRecordThreshold
	}
	if opts.ProjectLimit <= 0 {
		opts.ProjectLimit = defaultProjectionLimit
	}
	if opts.MaxRounds <= 0 {
		opts.MaxRounds = defaultProjectionMaxRounds
	}
	return opts
}

func (w *ProjectionWorker) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(w.opts.Interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.trigger(ctx, projectionTriggerTimer)
			case <-w.wakeCh:
				w.trigger(ctx, projectionTriggerCount)
			}
		}
	}()
}

func (w *ProjectionWorker) NotifyRecordAppended() {
	select {
	case w.wakeCh <- struct{}{}:
	default:
	}
}

func (w *ProjectionWorker) trigger(ctx context.Context, reason projectionTrigger) {
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer w.running.Store(false)
		if err := w.runProjection(ctx, reason); err != nil && w.opts.Logger != nil {
			w.opts.Logger.Warn("memory_projection_run_error", "reason", string(reason), "error", err.Error())
		}
	}()
}

func (w *ProjectionWorker) runProjection(ctx context.Context, reason projectionTrigger) error {
	if reason == projectionTriggerCount {
		hasEnough, err := w.hasAtLeastUnprojected(w.opts.NewRecordThreshold)
		if err != nil {
			return err
		}
		if !hasEnough {
			return nil
		}
	}

	for round := 0; round < w.opts.MaxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, projectErr := w.projector.ProjectOnce(ctx, w.opts.ProjectLimit)
		if projectErr != nil && w.opts.Logger != nil {
			w.opts.Logger.Warn("memory_projection_error", "reason", string(reason), "round", round+1, "error", projectErr.Error())
		}
		if result.Processed == 0 || result.Exhausted {
			return nil
		}
	}
	return nil
}

func (w *ProjectionWorker) hasAtLeastUnprojected(limit int) (bool, error) {
	cursor, err := w.loadCheckpointOffset()
	if err != nil {
		return false, err
	}
	count := 0
	_, _, err = w.journal.ReplayFrom(cursor, limit, func(rec memory.JournalRecord) error {
		count++
		return nil
	})
	if err != nil {
		return false, err
	}
	return count >= limit, nil
}

func (w *ProjectionWorker) loadCheckpointOffset() (memory.JournalCursor, error) {
	cp, ok, err := w.journal.LoadCheckpoint()
	if err != nil {
		return memory.JournalCursor{}, err
	}
	if !ok {
		return memory.JournalCursor{}, nil
	}
	return memory.JournalCursor{
		File: cp.File,
		Line: cp.Line,
		Byte: cp.Byte,
	}, nil
}
