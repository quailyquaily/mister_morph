package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
)

const defaultRuntimeGenerationPollInterval = time.Second

type RuntimeGenerationManagerOptions struct {
	Source       agentsettings.RuntimeConfigSource
	Build        func(context.Context, agentsettings.Reader) (ChannelRuntimeBundle, error)
	PollInterval time.Duration
	Logger       *slog.Logger
}

type RuntimeGenerationManager struct {
	mu             sync.Mutex
	reloadMu       sync.Mutex
	current        *runtimeGeneration
	nextID         uint64
	source         agentsettings.RuntimeConfigSource
	build          func(context.Context, agentsettings.Reader) (ChannelRuntimeBundle, error)
	pollInterval   time.Duration
	logger         *slog.Logger
	closed         bool
	startOnce      sync.Once
	cancelPoll     context.CancelFunc
	pollWG         sync.WaitGroup
	initialHash    runtimeConfigDigest
	hasInitialHash bool
}

type runtimeGeneration struct {
	id     uint64
	bundle ChannelRuntimeBundle
	reader agentsettings.Reader

	mu      sync.Mutex
	refs    int
	retired bool
	cleaned bool
}

type RuntimeGenerationLease struct {
	generation  *runtimeGeneration
	releaseOnce sync.Once
}

func NewRuntimeGenerationManager(ctx context.Context, opts RuntimeGenerationManagerOptions) (*RuntimeGenerationManager, error) {
	if opts.Source == nil {
		return nil, fmt.Errorf("runtime generation config source is required")
	}
	if opts.Build == nil {
		return nil, fmt.Errorf("runtime generation builder is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reader := opts.Source.CurrentReader()
	if reader == nil {
		return nil, fmt.Errorf("runtime generation config reader is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	initialHash, fingerprintErr := runtimeConfigFingerprint(opts.Source.ConfigPath())
	hasInitialHash := fingerprintErr == nil
	if fingerprintErr != nil {
		logger.Warn("channel_runtime_config_poll_failed", "error", fingerprintErr.Error())
	}
	bundle, err := opts.Build(ctx, reader)
	if err != nil {
		return nil, err
	}
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultRuntimeGenerationPollInterval
	}
	return &RuntimeGenerationManager{
		current:        newRuntimeGeneration(1, bundle, reader),
		nextID:         1,
		source:         opts.Source,
		build:          opts.Build,
		pollInterval:   pollInterval,
		logger:         logger,
		initialHash:    initialHash,
		hasInitialHash: hasInitialHash,
	}, nil
}

func NewStaticRuntimeGenerationManager(bundle ChannelRuntimeBundle, reader agentsettings.Reader) *RuntimeGenerationManager {
	return &RuntimeGenerationManager{
		current:      newRuntimeGeneration(1, bundle, reader),
		nextID:       1,
		pollInterval: defaultRuntimeGenerationPollInterval,
		logger:       slog.Default(),
	}
}

func newRuntimeGeneration(id uint64, bundle ChannelRuntimeBundle, reader agentsettings.Reader) *runtimeGeneration {
	return &runtimeGeneration{
		id:     id,
		bundle: bundle,
		reader: agentsettings.NewReaderSnapshot(reader),
	}
}

func (m *RuntimeGenerationManager) Capture() (*RuntimeGenerationLease, error) {
	if m == nil {
		return nil, fmt.Errorf("runtime generation manager is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.current == nil || !m.current.acquire() {
		return nil, fmt.Errorf("runtime generation is unavailable")
	}
	return &RuntimeGenerationLease{generation: m.current}, nil
}

func (m *RuntimeGenerationManager) Reload(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("runtime generation manager is nil")
	}
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if m.source == nil || m.build == nil {
		return fmt.Errorf("runtime generation reload is unavailable")
	}
	candidate, err := m.source.LoadCandidate()
	if err != nil {
		return err
	}
	if candidate == nil {
		return fmt.Errorf("runtime generation candidate reader is nil")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("runtime generation manager is closed")
	}
	currentReader := agentsettings.Reader(nil)
	if m.current != nil {
		currentReader = m.current.reader
	}
	m.mu.Unlock()
	if currentReader != nil && reflect.DeepEqual(currentReader.AllSettings(), candidate.AllSettings()) {
		return nil
	}
	bundle, err := m.build(ctx, candidate)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cleanupChannelRuntimeBundle(bundle)
		return fmt.Errorf("runtime generation manager is closed")
	}
	m.nextID++
	next := newRuntimeGeneration(m.nextID, bundle, candidate)
	previous := m.current
	m.source.ReplaceReader(candidate)
	m.current = next
	shouldCleanupPrevious := previous != nil && previous.retire()
	m.mu.Unlock()
	if shouldCleanupPrevious {
		previous.cleanup()
	}
	return nil
}

func (m *RuntimeGenerationManager) Start(ctx context.Context) {
	if m == nil || m.source == nil || strings.TrimSpace(m.source.ConfigPath()) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.startOnce.Do(func() {
		pollCtx, cancel := context.WithCancel(ctx)
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			cancel()
			return
		}
		m.cancelPoll = cancel
		m.mu.Unlock()
		m.pollWG.Add(1)
		go m.poll(pollCtx)
	})
}

func (m *RuntimeGenerationManager) poll(ctx context.Context) {
	defer m.pollWG.Done()
	path := strings.TrimSpace(m.source.ConfigPath())
	last := m.initialHash
	if !m.hasInitialHash {
		var err error
		last, err = runtimeConfigFingerprint(path)
		if err != nil {
			m.logger.Warn("channel_runtime_config_poll_failed", "error", err.Error())
		}
	}
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := runtimeConfigFingerprint(path)
			if err != nil {
				m.logger.Warn("channel_runtime_config_poll_failed", "error", err.Error())
				continue
			}
			if next == last {
				continue
			}
			last = next
			if err := m.Reload(ctx); err != nil {
				m.logger.Warn("channel_runtime_reload_failed", "error", err.Error())
				continue
			}
			m.logger.Info("channel_runtime_reloaded")
		}
	}
}

func (m *RuntimeGenerationManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	cancel := m.cancelPoll
	current := m.current
	m.current = nil
	shouldCleanup := current != nil && current.retire()
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.pollWG.Wait()
	if shouldCleanup {
		current.cleanup()
	}
}

func (l *RuntimeGenerationLease) Bundle() *ChannelRuntimeBundle {
	if l == nil || l.generation == nil {
		return nil
	}
	return &l.generation.bundle
}

func (l *RuntimeGenerationLease) Reader() agentsettings.Reader {
	if l == nil || l.generation == nil {
		return nil
	}
	return l.generation.reader
}

func (l *RuntimeGenerationLease) Generation() uint64 {
	if l == nil || l.generation == nil {
		return 0
	}
	return l.generation.id
}

func (l *RuntimeGenerationLease) Release() {
	if l == nil || l.generation == nil {
		return
	}
	l.releaseOnce.Do(func() {
		if l.generation.release() {
			l.generation.cleanup()
		}
	})
}

func (g *runtimeGeneration) acquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.retired || g.cleaned {
		return false
	}
	g.refs++
	return true
}

func (g *runtimeGeneration) release() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.refs > 0 {
		g.refs--
	}
	return g.retired && g.refs == 0 && !g.cleaned
}

func (g *runtimeGeneration) retire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.retired = true
	return g.refs == 0 && !g.cleaned
}

func (g *runtimeGeneration) cleanup() {
	g.mu.Lock()
	if g.cleaned {
		g.mu.Unlock()
		return
	}
	g.cleaned = true
	bundle := g.bundle
	g.mu.Unlock()
	cleanupChannelRuntimeBundle(bundle)
}

func cleanupChannelRuntimeBundle(bundle ChannelRuntimeBundle) {
	if bundle.Cleanup != nil {
		bundle.Cleanup()
	}
}

type runtimeConfigDigest struct {
	exists bool
	value  [sha256.Size]byte
}

func runtimeConfigFingerprint(path string) (runtimeConfigDigest, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return runtimeConfigDigest{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return runtimeConfigDigest{}, nil
		}
		return runtimeConfigDigest{}, err
	}
	return runtimeConfigDigest{exists: true, value: sha256.Sum256(data)}, nil
}
