package mixin

import (
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

func normalizeRunOptions(opts RunOptions) RunOptions {
	opts.KeystoreFile = strings.TrimSpace(opts.KeystoreFile)
	opts.AllowedConversationIDs = normalizeStrings(opts.AllowedConversationIDs)
	opts.GroupTriggerMode = strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	opts.FileCacheDir = strings.TrimSpace(opts.FileCacheDir)
	opts.ServerListen = strings.TrimSpace(opts.ServerListen)
	opts.ServerAuthToken = strings.TrimSpace(opts.ServerAuthToken)
	if opts.KeystoreFile != "" {
		opts.KeystoreFile = pathutil.ExpandHomePath(opts.KeystoreFile)
	}
	if opts.FileCacheDir == "" {
		opts.FileCacheDir = configdefaults.DefaultFileCacheDir
	}
	opts.FileCacheDir = pathutil.ExpandHomePath(opts.FileCacheDir)
	if opts.TaskTimeout <= 0 {
		opts.TaskTimeout = configdefaults.DefaultTaskTimeout
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = configdefaults.DefaultChannelMaxConcurrency
	}
	if opts.BusMaxInFlight <= 0 {
		opts.BusMaxInFlight = configdefaults.DefaultBusMaxInFlight
	}
	if opts.ServerMaxQueue <= 0 {
		opts.ServerMaxQueue = configdefaults.DefaultServerMaxQueue
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = configdefaults.DefaultLLMRequestTimeout
	}
	opts.AgentLimits = opts.AgentLimits.NormalizeForRuntime()
	if opts.GroupTriggerMode == "" {
		opts.GroupTriggerMode = "talkative"
	}
	if opts.ServerListen == "" && opts.TaskStore == nil {
		opts.ServerListen = "127.0.0.1:8792"
	}
	opts.AddressingConfidenceThreshold = normalizeThreshold(opts.AddressingConfidenceThreshold, configdefaults.DefaultAddressingThreshold)
	opts.AddressingInterjectThreshold = normalizeThreshold(opts.AddressingInterjectThreshold, configdefaults.DefaultAddressingThreshold)
	return opts
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func normalizeThreshold(value, fallback float64) float64 {
	if value <= 0 {
		value = fallback
	}
	if value > 1 {
		return 1
	}
	return value
}
