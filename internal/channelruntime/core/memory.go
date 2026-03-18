package core

import (
	"log/slog"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
)

type MemoryRuntimeOptions struct {
	Enabled       bool
	ShortTermDays int
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
	mgr := memory.NewManager(statepaths.MemoryDir(), opts.ShortTermDays)
	journal := mgr.NewJournal(memory.JournalOptions{})
	draftResolver, err := memoryruntime.NewConfiguredDraftResolver(memoryruntime.DraftResolverFactoryOptions{
		ResolveLLMRoute: d.ResolveLLMRoute,
		CreateLLMClient: d.CreateLLMClient,
		DecorateClient:  opts.Decorate,
	})
	if err != nil {
		_ = journal.Close()
		return MemoryRuntime{}, err
	}
	projector := memory.NewProjector(mgr, journal, memory.ProjectorOptions{
		DraftResolver: draftResolver,
	})
	orchestrator, err := memoryruntime.New(mgr, journal, projector, memoryruntime.OrchestratorOptions{})
	if err != nil {
		_ = journal.Close()
		return MemoryRuntime{}, err
	}
	projectionWorker, err := memoryruntime.NewProjectionWorker(journal, projector, memoryruntime.ProjectionWorkerOptions{
		Logger: opts.Logger,
	})
	if err != nil {
		_ = journal.Close()
		return MemoryRuntime{}, err
	}
	out.Orchestrator = orchestrator
	out.ProjectionWorker = projectionWorker
	out.Cleanup = func() {
		_ = journal.Close()
	}
	return out, nil
}
