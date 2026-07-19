package awareness

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
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
	CronNotify              CronNotifyFunc
	PokeRequests            <-chan PokeRequest
	CronRequests            <-chan CronRequest
	CronEnabled             bool
	CronPath                string
	ChatInfoContactsDir     string
	ChatInfoStore           *chatinfo.Store
	ChatInfoRefresher       chatinfo.Refresher
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
		CronNotify:              opts.CronNotify,
		PokeRequests:            opts.PokeRequests,
		CronRequests:            opts.CronRequests,
		CronEnabled:             opts.CronEnabled,
		CronPath:                strings.TrimSpace(opts.CronPath),
		ChatInfoContactsDir:     strings.TrimSpace(opts.ChatInfoContactsDir),
		ChatInfoStore:           opts.ChatInfoStore,
		ChatInfoRefresher:       opts.ChatInfoRefresher,
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
	if opts.ChatInfoContactsDir == "" {
		opts.ChatInfoContactsDir = statepaths.ContactsDir()
	}
	return opts
}
