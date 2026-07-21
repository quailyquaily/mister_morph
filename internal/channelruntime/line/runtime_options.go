package line

import (
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
)

func normalizeRunOptions(opts RunOptions) RunOptions {
	opts.ChannelAccessToken = strings.TrimSpace(opts.ChannelAccessToken)
	opts.ChannelSecret = strings.TrimSpace(opts.ChannelSecret)
	opts.AllowedGroupIDs = normalizeRunStringSlice(opts.AllowedGroupIDs)
	opts.GroupTriggerMode = strings.ToLower(strings.TrimSpace(opts.GroupTriggerMode))
	opts.FileCacheDir = strings.TrimSpace(opts.FileCacheDir)
	opts.ServerListen = strings.TrimSpace(opts.ServerListen)
	opts.ServerAuthToken = strings.TrimSpace(opts.ServerAuthToken)
	opts.BaseURL = strings.TrimSpace(opts.BaseURL)
	opts.WebhookListen = strings.TrimSpace(opts.WebhookListen)
	opts.WebhookPath = normalizeWebhookPath(opts.WebhookPath)

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
		opts.BaseURL = configdefaults.DefaultLineBaseURL
	}
	if opts.ServerListen == "" {
		opts.ServerListen = "127.0.0.1:8789"
	}
	if opts.WebhookListen == "" {
		opts.WebhookListen = configdefaults.DefaultLineWebhookListen
	}
	if opts.WebhookPath == "" {
		opts.WebhookPath = configdefaults.DefaultLineWebhookPath
	}
	if opts.FileCacheDir == "" {
		opts.FileCacheDir = configdefaults.DefaultFileCacheDir
	}
	opts.AddressingConfidenceThreshold = normalizeThreshold(opts.AddressingConfidenceThreshold, configdefaults.DefaultAddressingThreshold)
	opts.AddressingInterjectThreshold = normalizeThreshold(opts.AddressingInterjectThreshold, configdefaults.DefaultAddressingThreshold)
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

func normalizeWebhookPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func normalizeThreshold(v, fallback float64) float64 {
	if v <= 0 {
		v = fallback
	}
	if v > 1 {
		return 1
	}
	return v
}
