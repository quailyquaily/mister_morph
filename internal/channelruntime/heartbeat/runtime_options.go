package heartbeat

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
)

type runtimeLoopOptions struct {
	Interval                time.Duration
	InitialDelay            time.Duration
	TaskTimeout             time.Duration
	RequestTimeout          time.Duration
	AgentLimits             agent.Limits
	EngineToolsConfig       agent.EngineToolsConfig
	Source                  string
	ChecklistPath           string
	MemoryEnabled           bool
	MemoryShortTermDays     int
	MemoryInjectionEnabled  bool
	MemoryInjectionMaxItems int
	InspectPrompt           bool
	InspectRequest          bool
	Notifier                Notifier
	PokeRequests            <-chan PokeRequest
}

func resolveRuntimeLoopOptionsFromRunOptions(opts RunOptions) runtimeLoopOptions {
	out := runtimeLoopOptions{
		Interval:                opts.Interval,
		InitialDelay:            opts.InitialDelay,
		TaskTimeout:             opts.TaskTimeout,
		RequestTimeout:          opts.RequestTimeout,
		AgentLimits:             opts.AgentLimits,
		EngineToolsConfig:       opts.EngineToolsConfig,
		Source:                  strings.TrimSpace(opts.Source),
		ChecklistPath:           strings.TrimSpace(opts.ChecklistPath),
		MemoryEnabled:           opts.MemoryEnabled,
		MemoryShortTermDays:     opts.MemoryShortTermDays,
		MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
		InspectPrompt:           opts.InspectPrompt,
		InspectRequest:          opts.InspectRequest,
		Notifier:                opts.Notifier,
		PokeRequests:            opts.PokeRequests,
	}
	return normalizeRuntimeLoopOptions(out)
}

func normalizeRuntimeLoopOptions(opts runtimeLoopOptions) runtimeLoopOptions {
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Minute
	}
	if opts.InitialDelay <= 0 {
		opts.InitialDelay = 15 * time.Second
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
		opts.Source = "heartbeat"
	}
	if opts.ChecklistPath == "" {
		opts.ChecklistPath = statepaths.HeartbeatChecklistPath()
	}
	return opts
}
