package awareness

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

type runtimeLoopOptions struct {
	Interval                time.Duration
	TaskTimeout             time.Duration
	RequestTimeout          time.Duration
	AgentLimits             agent.Limits
	EngineToolsConfig       agent.EngineToolsConfig
	Source                  string
	ChecklistPath           string
	DisableHeartbeat        bool
	MemoryEnabled           bool
	MemoryShortTermDays     int
	MemoryInjectionEnabled  bool
	MemoryInjectionMaxItems int
	InspectPrompt           bool
	InspectRequest          bool
	Notifier                Notifier
	PokeRequests            <-chan PokeRequest
	CronEnabled             bool
	CronPath                string
	TaskStore               daemonruntime.TaskView
}

func resolveRuntimeLoopOptionsFromRunOptions(opts RunOptions) runtimeLoopOptions {
	out := runtimeLoopOptions{
		Interval:                opts.Interval,
		TaskTimeout:             opts.TaskTimeout,
		RequestTimeout:          opts.RequestTimeout,
		AgentLimits:             opts.AgentLimits,
		EngineToolsConfig:       opts.EngineToolsConfig,
		Source:                  strings.TrimSpace(opts.Source),
		ChecklistPath:           strings.TrimSpace(opts.ChecklistPath),
		DisableHeartbeat:        opts.DisableHeartbeat,
		MemoryEnabled:           opts.MemoryEnabled,
		MemoryShortTermDays:     opts.MemoryShortTermDays,
		MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
		InspectPrompt:           opts.InspectPrompt,
		InspectRequest:          opts.InspectRequest,
		Notifier:                opts.Notifier,
		PokeRequests:            opts.PokeRequests,
		CronEnabled:             opts.CronEnabled,
		CronPath:                strings.TrimSpace(opts.CronPath),
		TaskStore:               opts.TaskStore,
	}
	return normalizeRuntimeLoopOptions(out)
}

func normalizeRuntimeLoopOptions(opts runtimeLoopOptions) runtimeLoopOptions {
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Minute
	}
	if opts.TaskTimeout <= 0 {
		opts.TaskTimeout = 10 * time.Minute
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 90 * time.Second
	}
	if opts.MemoryShortTermDays <= 0 {
		opts.MemoryShortTermDays = 7
	}
	if opts.MemoryInjectionMaxItems <= 0 {
		opts.MemoryInjectionMaxItems = 50
	}
	opts.AgentLimits = opts.AgentLimits.NormalizeForRuntime()
	if opts.Source == "" {
		opts.Source = "awareness"
	}
	if opts.ChecklistPath == "" {
		opts.ChecklistPath = statepaths.HeartbeatChecklistPath()
	}
	if opts.CronPath == "" {
		opts.CronPath = statepaths.CronPath()
	}
	return opts
}
