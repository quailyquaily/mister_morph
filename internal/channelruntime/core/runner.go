package core

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

const defaultQueueSize = 16
const defaultConversationWorkerIdleTimeout = 5 * time.Minute

type conversationWorker[J any] struct {
	jobs           chan J
	version        uint64
	enqueueSenders int
}

type ConversationRunnerOptions[K comparable, J any] struct {
	IdleTimeout time.Duration
	Logger      *slog.Logger
	OnDrop      func(K, J)
	OnPanic     func(K, J)
}

type ConversationRunner[K comparable, J any] struct {
	workersCtx  context.Context
	sem         chan struct{}
	queueSize   int
	idleTimeout time.Duration
	logger      *slog.Logger
	handle      func(context.Context, K, J)
	onDrop      func(K, J)
	onPanic     func(K, J)

	enqueueGate sync.RWMutex
	workerWG    sync.WaitGroup
	shutdown    sync.Once
	closedDone  chan struct{}

	mu      sync.Mutex
	closed  bool
	workers map[K]*conversationWorker[J]
}

func NewConversationRunner[K comparable, J any](
	workersCtx context.Context,
	sem chan struct{},
	queueSize int,
	handle func(context.Context, K, J),
	opts ConversationRunnerOptions[K, J],
) *ConversationRunner[K, J] {
	if workersCtx == nil {
		workersCtx = context.Background()
	}
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	idleTimeout := opts.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultConversationWorkerIdleTimeout
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	runner := &ConversationRunner[K, J]{
		workersCtx:  workersCtx,
		sem:         sem,
		queueSize:   queueSize,
		idleTimeout: idleTimeout,
		logger:      logger,
		handle:      handle,
		onDrop:      opts.OnDrop,
		onPanic:     opts.OnPanic,
		closedDone:  make(chan struct{}),
		workers:     make(map[K]*conversationWorker[J]),
	}
	context.AfterFunc(workersCtx, runner.shutdownWorkers)
	return runner
}

func (r *ConversationRunner[K, J]) Enqueue(ctx context.Context, key K, buildJob func(version uint64) J) error {
	if buildJob == nil {
		return fmt.Errorf("build job callback is required")
	}
	if err := r.workersCtx.Err(); err != nil {
		return err
	}
	w, version, err := r.reserveWorkerWithVersion(key)
	if err != nil {
		return err
	}
	defer r.releaseWorkerSender(w)
	job := buildJob(version)
	if ctx == nil {
		ctx = r.workersCtx
	}

	r.enqueueGate.RLock()
	defer r.enqueueGate.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.workersCtx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return r.workersCtx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.workersCtx.Done():
		return r.workersCtx.Err()
	case w.jobs <- job:
		return nil
	}
}

func (r *ConversationRunner[K, J]) CurrentVersion(key K) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentVersionLocked(key)
}

func (r *ConversationRunner[K, J]) IncrementVersion(key K) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.workersCtx.Err() != nil {
		return r.currentVersionLocked(key)
	}
	w := r.ensureWorkerLocked(key)
	w.version++
	return w.version
}

func (r *ConversationRunner[K, J]) WaitClosed() {
	if r == nil {
		return
	}
	<-r.closedDone
}

func (r *ConversationRunner[K, J]) reserveWorkerWithVersion(key K) (*conversationWorker[J], uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		if err := r.workersCtx.Err(); err != nil {
			return nil, 0, err
		}
		return nil, 0, fmt.Errorf("conversation runner is closed")
	}
	w := r.ensureWorkerLocked(key)
	w.enqueueSenders++
	return w, w.version, nil
}

func (r *ConversationRunner[K, J]) releaseWorkerSender(w *conversationWorker[J]) {
	if r == nil || w == nil {
		return
	}
	r.mu.Lock()
	if w.enqueueSenders > 0 {
		w.enqueueSenders--
	}
	r.mu.Unlock()
}

func (r *ConversationRunner[K, J]) currentVersionLocked(key K) uint64 {
	w, ok := r.workers[key]
	if !ok || w == nil {
		return 0
	}
	return w.version
}

func (r *ConversationRunner[K, J]) ensureWorkerLocked(key K) *conversationWorker[J] {
	if w, ok := r.workers[key]; ok && w != nil {
		return w
	}
	w := &conversationWorker[J]{jobs: make(chan J, r.queueSize)}
	r.workers[key] = w
	r.workerWG.Add(1)
	go func() {
		defer r.workerWG.Done()
		r.runWorker(key, w)
	}()
	return w
}

func (r *ConversationRunner[K, J]) runWorker(key K, w *conversationWorker[J]) {
	timer := time.NewTimer(r.idleTimeout)
	defer timer.Stop()
	for {
		if r.workersCtx.Err() != nil {
			return
		}
		select {
		case <-r.workersCtx.Done():
			return
		case <-timer.C:
			if r.retireIdleWorker(key, w) {
				return
			}
			timer.Reset(r.idleTimeout)
		case job := <-w.jobs:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			select {
			case r.sem <- struct{}{}:
			case <-r.workersCtx.Done():
				r.dropJob(key, job)
				return
			}
			func() {
				defer func() { <-r.sem }()
				r.handleJob(key, job)
			}()
			timer.Reset(r.idleTimeout)
		}
	}
}

func (r *ConversationRunner[K, J]) shutdownWorkers() {
	r.shutdown.Do(func() {
		r.enqueueGate.Lock()
		r.mu.Lock()
		r.closed = true
		workers := make(map[K]*conversationWorker[J], len(r.workers))
		for key, worker := range r.workers {
			workers[key] = worker
		}
		r.mu.Unlock()
		r.enqueueGate.Unlock()

		r.workerWG.Wait()
		for key, worker := range workers {
			for {
				select {
				case job := <-worker.jobs:
					r.dropJob(key, job)
				default:
					goto drained
				}
			}
		drained:
		}

		r.mu.Lock()
		r.workers = nil
		r.mu.Unlock()
		close(r.closedDone)
	})
}

func (r *ConversationRunner[K, J]) dropJob(key K, job J) {
	if r.onDrop == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			r.logger.Error(
				"conversation_worker_drop_panic",
				"conversation_key", key,
				"panic", fmt.Sprint(recovered),
				"stack", string(debug.Stack()),
			)
		}
	}()
	r.onDrop(key, job)
}

func (r *ConversationRunner[K, J]) retireIdleWorker(key K, w *conversationWorker[J]) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.workers[key]
	if !ok || current != w || w.enqueueSenders != 0 || len(w.jobs) != 0 {
		return false
	}
	delete(r.workers, key)
	return true
}

func (r *ConversationRunner[K, J]) handleJob(key K, job J) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.logger.Error(
				"conversation_worker_job_panic",
				"conversation_key", key,
				"panic", fmt.Sprint(recovered),
				"stack", string(debug.Stack()),
			)
			if r.onPanic != nil {
				func() {
					defer func() {
						if callbackPanic := recover(); callbackPanic != nil {
							r.logger.Error(
								"conversation_worker_panic_callback_panic",
								"conversation_key", key,
								"panic", fmt.Sprint(callbackPanic),
								"stack", string(debug.Stack()),
							)
						}
					}()
					r.onPanic(key, job)
				}()
			}
		}
	}()
	if r.handle != nil {
		r.handle(r.workersCtx, key, job)
	}
}
