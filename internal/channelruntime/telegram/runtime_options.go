package telegram

import (
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
)

func normalizeRunOptions(opts RunOptions) RunOptions {
	opts.BotToken = strings.TrimSpace(opts.BotToken)
	opts.AllowedChatIDs = normalizeAllowedChatIDs(opts.AllowedChatIDs)
	opts.GroupTriggerMode = strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	opts.FileCacheDir = strings.TrimSpace(opts.FileCacheDir)
	opts.Server.Listen = strings.TrimSpace(opts.Server.Listen)
	opts.Server.AuthToken = strings.TrimSpace(opts.Server.AuthToken)

	if opts.PollTimeout <= 0 {
		opts.PollTimeout = configdefaults.DefaultTelegramPollTimeout
	}
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
	opts.AgentLimits = opts.AgentLimits.NormalizeForRuntime()
	if opts.FileCacheMaxAge <= 0 {
		opts.FileCacheMaxAge = configdefaults.DefaultFileCacheMaxAge
	}
	if opts.FileCacheMaxFiles <= 0 {
		opts.FileCacheMaxFiles = configdefaults.DefaultFileCacheMaxFiles
	}
	if opts.FileCacheMaxTotalBytes <= 0 {
		opts.FileCacheMaxTotalBytes = configdefaults.DefaultFileCacheMaxTotalBytes
	}
	if opts.FileCacheDir == "" {
		opts.FileCacheDir = configdefaults.DefaultFileCacheDir
	}
	if opts.GroupTriggerMode == "" {
		opts.GroupTriggerMode = configdefaults.DefaultGroupTriggerMode
	}
	if opts.Server.Listen == "" && opts.TaskStore == nil {
		opts.Server.Listen = "127.0.0.1:8787"
	}

	opts.AddressingConfidenceThreshold = normalizeAddressingThreshold(opts.AddressingConfidenceThreshold, configdefaults.DefaultAddressingThreshold)
	opts.AddressingInterjectThreshold = normalizeAddressingThreshold(opts.AddressingInterjectThreshold, configdefaults.DefaultAddressingThreshold)
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
