package core

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/domainjournal"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
)

type MemoryRuntimeOptions struct {
	Enabled       bool
	ShortTermDays int
	MemoryDir     string
	JournalDir    string
	Logger        *slog.Logger
	Decorate      func(client llm.Client, route llmutil.ResolvedRoute) llm.Client
}

type MemoryRuntime struct {
	Orchestrator     *memoryruntime.Orchestrator
	ProjectionWorker *memoryruntime.ProjectionWorker
	Cleanup          func()
}

func NewMemoryRuntime(d depsutil.CommonDependencies, opts MemoryRuntimeOptions) (MemoryRuntime, error) {
	out := MemoryRuntime{
		Cleanup: func() {},
	}
	if !opts.Enabled {
		return out, nil
	}
	memoryDir := strings.TrimSpace(opts.MemoryDir)
	if memoryDir == "" {
		memoryDir = strings.TrimSpace(d.RuntimePaths.MemoryDir)
	}
	if memoryDir == "" {
		return MemoryRuntime{}, fmt.Errorf("memory directory is required")
	}
	journalDir := strings.TrimSpace(opts.JournalDir)
	if journalDir == "" {
		journalDir = strings.TrimSpace(d.RuntimePaths.JournalDir)
	}
	if journalDir == "" {
		return MemoryRuntime{}, fmt.Errorf("journal directory is required")
	}
	mgr := memory.NewManager(memoryDir, opts.ShortTermDays)
	rawJournal, err := domainjournal.New(domainjournal.JournalOptions{
		Dir:           journalDir,
		SyncEachWrite: true,
	})
	if err != nil {
		return MemoryRuntime{}, err
	}
	journal := memory.NewDomainJournal(memoryDir, rawJournal)
	draftResolver, err := memoryruntime.NewConfiguredDraftResolver(memoryruntime.DraftResolverFactoryOptions{
		ResolveLLMRoute: d.ResolveLLMRoute,
		CreateLLMClient: d.CreateLLMClient,
		DecorateClient:  opts.Decorate,
	})
	if err != nil {
		_ = rawJournal.Close()
		return MemoryRuntime{}, err
	}
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			if closer, ok := draftResolver.(io.Closer); ok {
				_ = closer.Close()
			}
			_ = rawJournal.Close()
		})
	}
	projector := memory.NewProjector(mgr, journal, memory.ProjectorOptions{
		DraftResolver: draftResolver,
	})
	orchestrator, err := memoryruntime.New(mgr, journal, projector, memoryruntime.OrchestratorOptions{})
	if err != nil {
		cleanup()
		return MemoryRuntime{}, err
	}
	projectionWorker, err := memoryruntime.NewProjectionWorker(journal, projector, memoryruntime.ProjectionWorkerOptions{
		Logger: opts.Logger,
	})
	if err != nil {
		cleanup()
		return MemoryRuntime{}, err
	}
	out.Orchestrator = orchestrator
	out.ProjectionWorker = projectionWorker
	out.Cleanup = cleanup
	return out, nil
}
