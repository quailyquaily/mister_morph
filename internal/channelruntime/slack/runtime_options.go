package slack

import (
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

func normalizeRunOptions(opts RunOptions) RunOptions {
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
		opts.TaskTimeout = configdefaults.DefaultTaskTimeout
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = configdefaults.DefaultChannelMaxConcurrency
	}
	if opts.BusMaxInFlight <= 0 {
		opts.BusMaxInFlight = configdefaults.DefaultBusMaxInFlight
	}
	if opts.Server.MaxQueue <= 0 {
		opts.Server.MaxQueue = configdefaults.DefaultServerMaxQueue
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = configdefaults.DefaultLLMRequestTimeout
	}
	if opts.MemoryShortTermDays <= 0 {
		opts.MemoryShortTermDays = configdefaults.DefaultMemoryShortTermDays
	}
	if opts.MemoryInjectionMaxItems <= 0 {
		opts.MemoryInjectionMaxItems = configdefaults.DefaultMemoryInjectionMaxItems
	}
	opts.AgentLimits = opts.AgentLimits.NormalizeForRuntime()
	if opts.GroupTriggerMode == "" {
		opts.GroupTriggerMode = configdefaults.DefaultGroupTriggerMode
	}
	if opts.BaseURL == "" {
		opts.BaseURL = configdefaults.DefaultSlackBaseURL
	}
	if opts.FileCacheDir == "" {
		opts.FileCacheDir = configdefaults.DefaultFileCacheDir
	}
	opts.FileCacheDir = pathutil.ExpandHomePath(opts.FileCacheDir)
	if opts.Server.Listen == "" && opts.TaskStore == nil {
		opts.Server.Listen = "127.0.0.1:8788"
	}
	opts.AddressingConfidenceThreshold = normalizeThreshold(opts.AddressingConfidenceThreshold, configdefaults.DefaultAddressingThreshold, configdefaults.DefaultAddressingThreshold)
	opts.AddressingInterjectThreshold = normalizeThreshold(opts.AddressingInterjectThreshold, configdefaults.DefaultAddressingThreshold, configdefaults.DefaultAddressingThreshold)
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
