package slack

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type runtimeLoopOptions struct {
	BotToken                      string
	AppToken                      string
	AllowedTeamIDs                []string
	AllowedChannelIDs             []string
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	FileCacheDir                  string
	Server                        ServerOptions
	Hooks                         Hooks
	BaseURL                       string
	BusMaxInFlight                int
	RequestTimeout                time.Duration
	AgentLimits                   agent.Limits
	MemoryEnabled                 bool
	MemoryShortTermDays           int
	MemoryInjectionEnabled        bool
	MemoryInjectionMaxItems       int
	InspectPrompt                 bool
	InspectRequest                bool
	TaskStore                     daemonruntime.TaskView
}

func resolveRuntimeLoopOptionsFromRunOptions(opts RunOptions) runtimeLoopOptions {
	out := runtimeLoopOptions{
		BotToken:                      strings.TrimSpace(opts.BotToken),
		AppToken:                      strings.TrimSpace(opts.AppToken),
		AllowedTeamIDs:                normalizeRunStringSlice(opts.AllowedTeamIDs),
		AllowedChannelIDs:             normalizeRunStringSlice(opts.AllowedChannelIDs),
		GroupTriggerMode:              strings.TrimSpace(opts.GroupTriggerMode),
		AddressingConfidenceThreshold: opts.AddressingConfidenceThreshold,
		AddressingInterjectThreshold:  opts.AddressingInterjectThreshold,
		TaskTimeout:                   opts.TaskTimeout,
		MaxConcurrency:                opts.MaxConcurrency,
		FileCacheDir:                  strings.TrimSpace(opts.FileCacheDir),
		Server: ServerOptions{
			Listen:    strings.TrimSpace(opts.Server.Listen),
			AuthToken: strings.TrimSpace(opts.Server.AuthToken),
			MaxQueue:  opts.Server.MaxQueue,
			Poke:      opts.Server.Poke,
		},
		BaseURL:                 strings.TrimSpace(opts.BaseURL),
		Hooks:                   opts.Hooks,
		BusMaxInFlight:          opts.BusMaxInFlight,
		RequestTimeout:          opts.RequestTimeout,
		AgentLimits:             opts.AgentLimits,
		MemoryEnabled:           opts.MemoryEnabled,
		MemoryShortTermDays:     opts.MemoryShortTermDays,
		MemoryInjectionEnabled:  opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: opts.MemoryInjectionMaxItems,
		InspectPrompt:           opts.InspectPrompt,
		InspectRequest:          opts.InspectRequest,
		TaskStore:               opts.TaskStore,
	}
	return normalizeRuntimeLoopOptions(out)
}

func normalizeRuntimeLoopOptions(opts runtimeLoopOptions) runtimeLoopOptions {
	opts.BotToken = strings.TrimSpace(opts.BotToken)
	opts.AppToken = strings.TrimSpace(opts.AppToken)
	opts.AllowedTeamIDs = normalizeRunStringSlice(opts.AllowedTeamIDs)
	opts.AllowedChannelIDs = normalizeRunStringSlice(opts.AllowedChannelIDs)
	opts.GroupTriggerMode = strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	opts.FileCacheDir = strings.TrimSpace(opts.FileCacheDir)
	opts.Server.Listen = strings.TrimSpace(opts.Server.Listen)
	opts.Server.AuthToken = strings.TrimSpace(opts.Server.AuthToken)
	opts.BaseURL = strings.TrimSpace(opts.BaseURL)

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
	if opts.MemoryShortTermDays <= 0 {
		opts.MemoryShortTermDays = 7
	}
	if opts.MemoryInjectionMaxItems <= 0 {
		opts.MemoryInjectionMaxItems = 50
	}
	opts.AgentLimits = opts.AgentLimits.NormalizeForRuntime()
	if opts.GroupTriggerMode == "" {
		opts.GroupTriggerMode = "smart"
	}
	if opts.BaseURL == "" {
		opts.BaseURL = "https://slack.com/api"
	}
	if opts.FileCacheDir == "" {
		opts.FileCacheDir = "~/.cache/morph"
	}
	opts.FileCacheDir = pathutil.ExpandHomePath(opts.FileCacheDir)
	if opts.Server.Listen == "" && opts.TaskStore == nil {
		opts.Server.Listen = "127.0.0.1:8787"
	}
	opts.AddressingConfidenceThreshold = normalizeThreshold(opts.AddressingConfidenceThreshold, 0.6, 0.6)
	opts.AddressingInterjectThreshold = normalizeThreshold(opts.AddressingInterjectThreshold, 0.6, 0.6)
	return opts
}

func normalizeRunStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}
