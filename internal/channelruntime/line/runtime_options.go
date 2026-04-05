package line

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
)

type runtimeLoopOptions struct {
	ChannelAccessToken            string
	ChannelSecret                 string
	AllowedGroupIDs               []string
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	FileCacheDir                  string
	ServerListen                  string
	ServerAuthToken               string
	ServerMaxQueue                int
	BaseURL                       string
	WebhookListen                 string
	WebhookPath                   string
	BusMaxInFlight                int
	RequestTimeout                time.Duration
	AgentLimits                   agent.Limits
	EngineToolsConfig             agent.EngineToolsConfig
	MemoryEnabled                 bool
	MemoryShortTermDays           int
	MemoryInjectionEnabled        bool
	MemoryInjectionMaxItems       int
	ImageRecognitionEnabled       bool
	Hooks                         Hooks
	InspectPrompt                 bool
	InspectRequest                bool
}

func resolveRuntimeLoopOptionsFromRunOptions(opts RunOptions) runtimeLoopOptions {
	out := runtimeLoopOptions{
		ChannelAccessToken:            strings.TrimSpace(opts.ChannelAccessToken),
		ChannelSecret:                 strings.TrimSpace(opts.ChannelSecret),
		AllowedGroupIDs:               normalizeRunStringSlice(opts.AllowedGroupIDs),
		GroupTriggerMode:              strings.TrimSpace(opts.GroupTriggerMode),
		AddressingConfidenceThreshold: opts.AddressingConfidenceThreshold,
		AddressingInterjectThreshold:  opts.AddressingInterjectThreshold,
		TaskTimeout:                   opts.TaskTimeout,
		MaxConcurrency:                opts.MaxConcurrency,
		FileCacheDir:                  strings.TrimSpace(opts.FileCacheDir),
		ServerListen:                  strings.TrimSpace(opts.ServerListen),
		ServerAuthToken:               strings.TrimSpace(opts.ServerAuthToken),
		ServerMaxQueue:                opts.ServerMaxQueue,
		BaseURL:                       strings.TrimSpace(opts.BaseURL),
		WebhookListen:                 strings.TrimSpace(opts.WebhookListen),
		WebhookPath:                   strings.TrimSpace(opts.WebhookPath),
		BusMaxInFlight:                opts.BusMaxInFlight,
		RequestTimeout:                opts.RequestTimeout,
		AgentLimits:                   opts.AgentLimits,
		EngineToolsConfig:             opts.EngineToolsConfig,
		MemoryEnabled:                 opts.MemoryEnabled,
		MemoryShortTermDays:           opts.MemoryShortTermDays,
		MemoryInjectionEnabled:        opts.MemoryInjectionEnabled,
		MemoryInjectionMaxItems:       opts.MemoryInjectionMaxItems,
		ImageRecognitionEnabled:       opts.ImageRecognitionEnabled,
		Hooks:                         opts.Hooks,
		InspectPrompt:                 opts.InspectPrompt,
		InspectRequest:                opts.InspectRequest,
	}
	return normalizeRuntimeLoopOptions(out)
}

func normalizeRuntimeLoopOptions(opts runtimeLoopOptions) runtimeLoopOptions {
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
		opts.TaskTimeout = 10 * time.Minute
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = 3
	}
	if opts.BusMaxInFlight <= 0 {
		opts.BusMaxInFlight = 1024
	}
	if opts.ServerMaxQueue <= 0 {
		opts.ServerMaxQueue = 100
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
		opts.BaseURL = "https://api.line.me"
	}
	if opts.ServerListen == "" {
		opts.ServerListen = "127.0.0.1:8787"
	}
	if opts.WebhookListen == "" {
		opts.WebhookListen = "127.0.0.1:18080"
	}
	if opts.WebhookPath == "" {
		opts.WebhookPath = "/line/webhook"
	}
	if opts.FileCacheDir == "" {
		opts.FileCacheDir = "~/.cache/morph"
	}
	opts.AddressingConfidenceThreshold = normalizeThreshold(opts.AddressingConfidenceThreshold, 0.6)
	opts.AddressingInterjectThreshold = normalizeThreshold(opts.AddressingInterjectThreshold, 0.6)
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
