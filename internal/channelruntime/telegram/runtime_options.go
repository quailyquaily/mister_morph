package telegram

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

type runtimeLoopOptions struct {
	BotToken                      string
	AllowedChatIDs                []int64
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	PollTimeout                   time.Duration
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	FileCacheDir                  string
	Server                        ServerOptions
	Hooks                         Hooks
	BusMaxInFlight                int
	RequestTimeout                time.Duration
	AgentLimits                   agent.Limits
	EngineToolsConfig             agent.EngineToolsConfig
	FileCacheMaxAge               time.Duration
	FileCacheMaxFiles             int
	FileCacheMaxTotalBytes        int64
	MemoryEnabled                 bool
	MemoryShortTermDays           int
	MemoryInjectionEnabled        bool
	MemoryInjectionMaxItems       int
	ImageRecognitionEnabled       bool
	InspectPrompt                 bool
	InspectRequest                bool
	TaskStore                     daemonruntime.TaskView
}

func resolveRuntimeLoopOptionsFromRunOptions(opts RunOptions) runtimeLoopOptions {
	out := runtimeLoopOptions{
		BotToken:                      strings.TrimSpace(opts.BotToken),
		AllowedChatIDs:                normalizeAllowedChatIDs(opts.AllowedChatIDs),
		GroupTriggerMode:              strings.TrimSpace(opts.GroupTriggerMode),
		AddressingConfidenceThreshold: opts.AddressingConfidenceThreshold,
		AddressingInterjectThreshold:  opts.AddressingInterjectThreshold,
		PollTimeout:                   opts.PollTimeout,
		TaskTimeout:                   opts.TaskTimeout,
		MaxConcurrency:                opts.MaxConcurrency,
		FileCacheDir:                  strings.TrimSpace(opts.FileCacheDir),
		Server: ServerOptions{
			Listen:    strings.TrimSpace(opts.Server.Listen),
			AuthToken: strings.TrimSpace(opts.Server.AuthToken),
			MaxQueue:  opts.Server.MaxQueue,
			Poke:      opts.Server.Poke,
		},
		Hooks:                   opts.Hooks,
		BusMaxInFlight:          opts.BusMaxInFlight,
		RequestTimeout:          opts.RequestTimeout,
		AgentLimits:             opts.AgentLimits,
		EngineToolsConfig:       opts.EngineToolsConfig,
		FileCacheMaxAge:         opts.FileCacheMaxAge,
		FileCacheMaxFiles:       opts.FileCacheMaxFiles,
		FileCacheMaxTotalBytes:  opts.FileCacheMaxTotalBytes,
		MemoryEnabled:           opts.MemoryEnabled,
		MemoryShortTermDays:     opts.MemoryShortTermDays,
		MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
		ImageRecognitionEnabled: opts.ImageRecognitionEnabled,
		InspectPrompt:           opts.InspectPrompt,
		InspectRequest:          opts.InspectRequest,
		TaskStore:               opts.TaskStore,
	}
	return normalizeRuntimeLoopOptions(out)
}

func normalizeRuntimeLoopOptions(opts runtimeLoopOptions) runtimeLoopOptions {
	opts.BotToken = strings.TrimSpace(opts.BotToken)
	opts.AllowedChatIDs = normalizeAllowedChatIDs(opts.AllowedChatIDs)
	opts.GroupTriggerMode = strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	opts.FileCacheDir = strings.TrimSpace(opts.FileCacheDir)
	opts.Server.Listen = strings.TrimSpace(opts.Server.Listen)
	opts.Server.AuthToken = strings.TrimSpace(opts.Server.AuthToken)

	if opts.PollTimeout <= 0 {
		opts.PollTimeout = 30 * time.Second
	}
	if opts.TaskTimeout <= 0 {
		opts.TaskTimeout = 10 * time.Minute
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = 3
	}
	if opts.BusMaxInFlight <= 0 {
		opts.BusMaxInFlight = 1024
	}
	if opts.Server.MaxQueue <= 0 {
		opts.Server.MaxQueue = 100
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 90 * time.Second
	}
	opts.AgentLimits = opts.AgentLimits.NormalizeForRuntime()
	if opts.FileCacheMaxAge <= 0 {
		opts.FileCacheMaxAge = 7 * 24 * time.Hour
	}
	if opts.FileCacheMaxFiles <= 0 {
		opts.FileCacheMaxFiles = 1000
	}
	if opts.FileCacheMaxTotalBytes <= 0 {
		opts.FileCacheMaxTotalBytes = int64(512 * 1024 * 1024)
	}
	if opts.MemoryShortTermDays <= 0 {
		opts.MemoryShortTermDays = 7
	}
	if opts.MemoryInjectionMaxItems <= 0 {
		opts.MemoryInjectionMaxItems = 50
	}
	if opts.FileCacheDir == "" {
		opts.FileCacheDir = "~/.cache/morph"
	}
	if opts.GroupTriggerMode == "" {
		opts.GroupTriggerMode = "smart"
	}
	if opts.Server.Listen == "" && opts.TaskStore == nil {
		opts.Server.Listen = "127.0.0.1:8787"
	}

	opts.AddressingConfidenceThreshold = normalizeAddressingThreshold(opts.AddressingConfidenceThreshold, 0.6)
	opts.AddressingInterjectThreshold = normalizeAddressingThreshold(opts.AddressingInterjectThreshold, 0.6)
	return opts
}

func normalizeAddressingThreshold(v float64, fallback float64) float64 {
	if v <= 0 {
		v = fallback
	}
	if v > 1 {
		v = 1
	}
	return v
}
