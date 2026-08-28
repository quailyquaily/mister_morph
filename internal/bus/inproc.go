package bus

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

var (
	ErrBusClosed            = errors.New("bus is closed")
	ErrNoSubscriberForTopic = errors.New("no subscriber for topic")
	ErrTopicAlreadyHandled  = errors.New("topic already has handler")
	ErrTopicFrozen          = errors.New("topic registry is frozen")
)

type HandlerFunc func(ctx context.Context, msg BusMessage) error

type DeliveryError struct {
	Message    BusMessage `json:"message"`
	Topic      string     `json:"topic"`
	ErrorText  string     `json:"error"`
	OccurredAt time.Time  `json:"occurred_at"`
}

type InprocOptions struct {
	MaxInFlight int
	Logger      *slog.Logger
}

type queuedDelivery struct {
	message   BusMessage
	ownsToken bool
	result    chan error
}

type deliveryContextKey struct{}

type deliveryScope struct {
	mu     sync.Mutex
	bus    *Inproc
	active bool
}

type Inproc struct {
	maxInFlight int
	logger      *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	closing   chan struct{}
	done      chan struct{}
	tokens    chan struct{}
	errs      chan DeliveryError
	closeOnce sync.Once
	enqueueMu sync.RWMutex
	delivery  sync.WaitGroup

	mu          sync.RWMutex
	closed      bool
	started     bool
	subscribers map[string]HandlerFunc
	shards      []chan queuedDelivery

	wg sync.WaitGroup
}

func NewInproc(opts InprocOptions) (*Inproc, error) {
	if opts.MaxInFlight <= 0 {
		return nil, fmt.Errorf("max_inflight must be > 0")
	}
	if opts.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	shardCount := deriveShardCount(opts.MaxInFlight)
	logger := opts.Logger
	ctx, cancel := context.WithCancel(context.Background())
	shards := make([]chan queuedDelivery, shardCount)
	for i := range shards {
		shards[i] = make(chan queuedDelivery, opts.MaxInFlight)
	}
	b := &Inproc{
		maxInFlight: opts.MaxInFlight,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		closing:     make(chan struct{}),
		done:        make(chan struct{}),
		tokens:      make(chan struct{}, opts.MaxInFlight),
		errs:        make(chan DeliveryError, opts.MaxInFlight),
		subscribers: make(map[string]HandlerFunc),
		shards:      shards,
	}
	for i := 0; i < opts.MaxInFlight; i++ {
		b.tokens <- struct{}{}
	}
	b.logger.Debug(
		"bus_inproc_initialized",
		"max_inflight", opts.MaxInFlight,
		"shard_count", shardCount,
		"shard_queue_capacity", opts.MaxInFlight,
	)
	for shard := range b.shards {
		b.wg.Add(1)
		go b.runShardWorker(shard, b.shards[shard])
	}
	return b, nil
}

// Errors reports delivery failures on a best-effort basis. A slow consumer
// may miss errors; durable task state and structured logs remain authoritative.
func (b *Inproc) Errors() <-chan DeliveryError {
	return b.errs
}

func (b *Inproc) Subscribe(topic string, handler HandlerFunc) error {
	if handler == nil {
		return fmt.Errorf("handler is required")
	}
	if err := ValidateTopic(topic); err != nil {
		return wrapError(CodeInvalidTopic, err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || channelClosed(b.closing) {
		return wrapError(CodeBusClosed, ErrBusClosed)
	}
	if b.started {
		return wrapError(CodeTopicFrozen, ErrTopicFrozen)
	}
	if _, exists := b.subscribers[topic]; exists {
		return wrapError(CodeTopicAlreadyHandled, fmt.Errorf("%w: %s", ErrTopicAlreadyHandled, topic))
	}
	b.subscribers[topic] = handler
	b.logger.Debug("bus_subscribe", "topic", topic)
	return nil
}

func (b *Inproc) PublishValidated(ctx context.Context, msg BusMessage) error {
	if err := msg.Validate(); err != nil {
		return wrapError(CodeInvalidMessage, err)
	}
	return b.Publish(ctx, msg)
}

func (b *Inproc) Publish(ctx context.Context, msg BusMessage) error {
	return b.publish(ctx, msg, nil)
}

// PublishValidatedAndWait returns only after the topic handler has completed.
// It is intended for external transports that must not acknowledge a message
// before its durable task handoff succeeds.
func (b *Inproc) PublishValidatedAndWait(ctx context.Context, msg BusMessage) error {
	if err := msg.Validate(); err != nil {
		return wrapError(CodeInvalidMessage, err)
	}
	result := make(chan error, 1)
	if err := b.publish(ctx, msg, result); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.done:
		return wrapError(CodeBusClosed, ErrBusClosed)
	case err := <-result:
		return err
	}
}

func (b *Inproc) publish(ctx context.Context, msg BusMessage, result chan error) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	b.enqueueMu.RLock()
	defer b.enqueueMu.RUnlock()
	// A handler that publishes another message must reuse the handler context or
	// a derived context. This identifies the publish as part of the active
	// delivery, avoids waiting on its own external backpressure token, and lets
	// graceful Close drain the derived message.
	deliveryScope := b.claimActiveDeliveryContext(ctx)
	internal := deliveryScope != nil
	if deliveryScope != nil {
		defer deliveryScope.mu.Unlock()
	}
	if result != nil && internal {
		return fmt.Errorf("synchronous bus publish from a handler is not supported")
	}
	if !internal && channelClosed(b.closing) {
		return wrapError(CodeBusClosed, ErrBusClosed)
	}
	select {
	case <-b.done:
		return wrapError(CodeBusClosed, ErrBusClosed)
	default:
	}
	// Publish runs on the internal fast path and expects boundary adapters
	// to validate BusMessage before calling into the bus.
	if err := b.preparePublish(msg.Topic); err != nil {
		return err
	}

	shardIndex, shardQueue, err := b.shardQueue(msg.ConversationKey)
	if err != nil {
		return err
	}
	b.logger.Debug("bus_publish_start",
		"id", msg.ID,
		"topic", msg.Topic,
		"channel", msg.Channel,
		"conversation_key", msg.ConversationKey,
		"idempotency_key", msg.IdempotencyKey,
		"correlation_id", msg.CorrelationID,
		"shard", shardIndex,
		"shard_queue_depth", len(shardQueue),
		"in_flight", b.maxInFlight-len(b.tokens),
	)
	if !internal && len(b.tokens) == 0 {
		b.logger.Debug("bus_publish_backpressure_wait",
			"id", msg.ID,
			"topic", msg.Topic,
			"conversation_key", msg.ConversationKey,
			"shard", shardIndex,
		)
	}

	ownsToken := !internal
	if ownsToken {
		select {
		case <-ctx.Done():
			return publishCtxError(ctx.Err())
		case <-b.closing:
			return wrapError(CodeBusClosed, ErrBusClosed)
		case <-b.done:
			return wrapError(CodeBusClosed, ErrBusClosed)
		case <-b.tokens:
		}
	}

	delivery := queuedDelivery{message: msg, ownsToken: ownsToken, result: result}
	b.delivery.Add(1)
	enqueued := false
	defer func() {
		if enqueued {
			return
		}
		b.delivery.Done()
		if ownsToken {
			b.releaseToken()
		}
	}()
	if internal {
		select {
		case <-ctx.Done():
			return publishCtxError(ctx.Err())
		case <-b.done:
			return wrapError(CodeBusClosed, ErrBusClosed)
		case shardQueue <- delivery:
			enqueued = true
		default:
			return wrapError(CodeQueueFull, fmt.Errorf("bus shard queue is full"))
		}
	} else {
		select {
		case <-ctx.Done():
			return publishCtxError(ctx.Err())
		case <-b.closing:
			return wrapError(CodeBusClosed, ErrBusClosed)
		case <-b.done:
			return wrapError(CodeBusClosed, ErrBusClosed)
		case shardQueue <- delivery:
			enqueued = true
		}
	}
	if enqueued {
		b.logger.Debug("bus_publish_enqueued",
			"id", msg.ID,
			"topic", msg.Topic,
			"conversation_key", msg.ConversationKey,
			"shard", shardIndex,
			"shard_queue_depth", len(shardQueue),
			"in_flight", b.maxInFlight-len(b.tokens),
		)
		return nil
	}
	return wrapError(CodeQueueFull, fmt.Errorf("bus shard queue is full"))
}

func publishCtxError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return wrapError(CodeQueueFull, err)
	}
	return err
}

// Close rejects new publishes and waits for every accepted delivery handler to
// return. Callers must stop external ingress before calling Close. Close must
// not be called by a handler, and handlers must not wait for their bus context
// to be canceled before returning.
func (b *Inproc) Close() error {
	b.closeOnce.Do(func() {
		// Stop external publishers, then establish that every external publish
		// accepted before this boundary has entered a shard queue. An active
		// handler may still publish derived messages while it owns its delivery
		// context; those messages join the same delivery wait group.
		close(b.closing)
		b.enqueueMu.Lock()
		b.enqueueMu.Unlock()
		b.logger.Debug("bus_close_requested")

		b.delivery.Wait()

		// No handler remains, so no valid internal publisher can add another
		// delivery. Close shard queues only after all publishers have left the
		// enqueue boundary.
		b.enqueueMu.Lock()
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()
		for _, shard := range b.shards {
			close(shard)
		}
		close(b.done)
		b.enqueueMu.Unlock()
		b.wg.Wait()
		b.cancel()
		close(b.errs)
		b.logger.Debug("bus_closed")
	})
	return nil
}

func (b *Inproc) preparePublish(topic string) error {
	if err := ValidateTopic(topic); err != nil {
		return wrapError(CodeInvalidTopic, err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return wrapError(CodeBusClosed, ErrBusClosed)
	}
	if !b.started {
		b.started = true
		b.logger.Debug("bus_topic_registry_frozen")
	}
	handler, ok := b.subscribers[topic]
	if !ok || handler == nil {
		return wrapError(CodeNoSubscriber, fmt.Errorf("%w: %s", ErrNoSubscriberForTopic, topic))
	}
	return nil
}

func (b *Inproc) shardQueue(conversationKey string) (int, chan queuedDelivery, error) {
	if err := validateRequiredCanonicalString("conversation_key", conversationKey); err != nil {
		return 0, nil, err
	}
	if len(b.shards) == 0 {
		return 0, nil, fmt.Errorf("bus shards are not initialized")
	}
	index := shardIndexFor(conversationKey, len(b.shards))
	return index, b.shards[index], nil
}

func (b *Inproc) runShardWorker(index int, queue chan queuedDelivery) {
	defer b.wg.Done()
	b.logger.Debug("bus_shard_worker_started", "shard", index)
	for queued := range queue {
		func() {
			defer b.delivery.Done()
			if queued.ownsToken {
				defer b.releaseToken()
			}
			msg := queued.message
			err := b.deliver(index, msg)
			if queued.result != nil {
				queued.result <- err
			}
			if err != nil {
				b.reportDeliveryError(msg, err)
			}
		}()
	}
	b.logger.Debug("bus_shard_worker_stopped", "shard", index)
}

func (b *Inproc) deliver(shard int, msg BusMessage) (err error) {
	handler, err := b.subscriberForTopic(msg.Topic)
	if err != nil {
		return err
	}
	scope := &deliveryScope{bus: b, active: true}
	deliveryCtx := context.WithValue(b.ctx, deliveryContextKey{}, scope)
	defer func() {
		scope.mu.Lock()
		scope.active = false
		scope.mu.Unlock()
		if recovered := recover(); recovered != nil {
			b.logger.Error("bus_handler_panic",
				"id", msg.ID,
				"topic", msg.Topic,
				"conversation_key", msg.ConversationKey,
				"panic", recovered,
				"stack", string(debug.Stack()),
			)
			err = fmt.Errorf("bus handler panic: %v", recovered)
		}
	}()
	err = handler(deliveryCtx, msg)
	if err != nil {
		return err
	}
	b.logger.Debug("bus_deliver_ok",
		"id", msg.ID,
		"topic", msg.Topic,
		"channel", msg.Channel,
		"conversation_key", msg.ConversationKey,
		"idempotency_key", msg.IdempotencyKey,
		"correlation_id", msg.CorrelationID,
		"shard", shard,
	)
	return nil
}

// claimActiveDeliveryContext returns a locked scope. Holding the lock until
// delivery.Add makes the active check and child-delivery reservation atomic
// with the parent handler's return path.
func (b *Inproc) claimActiveDeliveryContext(ctx context.Context) *deliveryScope {
	if b == nil || ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(deliveryContextKey{}).(*deliveryScope)
	if scope == nil || scope.bus != b {
		return nil
	}
	scope.mu.Lock()
	if !scope.active {
		scope.mu.Unlock()
		return nil
	}
	return scope
}

func (b *Inproc) subscriberForTopic(topic string) (HandlerFunc, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	handler, ok := b.subscribers[topic]
	if !ok {
		return nil, wrapError(CodeNoSubscriber, fmt.Errorf("%w: %s", ErrNoSubscriberForTopic, topic))
	}
	return handler, nil
}

func (b *Inproc) releaseToken() {
	select {
	case b.tokens <- struct{}{}:
	case <-b.done:
	}
}

func (b *Inproc) reportDeliveryError(msg BusMessage, err error) {
	b.logger.Warn("bus_deliver_failed",
		"id", msg.ID,
		"topic", msg.Topic,
		"channel", msg.Channel,
		"conversation_key", msg.ConversationKey,
		"idempotency_key", msg.IdempotencyKey,
		"correlation_id", msg.CorrelationID,
		"error", err.Error(),
	)
	select {
	case <-b.done:
	case b.errs <- DeliveryError{
		Message:    msg,
		Topic:      msg.Topic,
		ErrorText:  err.Error(),
		OccurredAt: time.Now().UTC(),
	}:
	default:
	}
}

func channelClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func deriveShardCount(maxInFlight int) int {
	const defaultShardCount = 16
	if maxInFlight <= defaultShardCount {
		return maxInFlight
	}
	return defaultShardCount
}

func shardIndexFor(conversationKey string, shardCount int) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(conversationKey))
	return int(hasher.Sum32() % uint32(shardCount))
}
