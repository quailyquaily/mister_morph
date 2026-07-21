package awareness

import (
	"strings"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
)

func normalizeRunOptions(opts RunOptions, paths runtimepaths.Paths) RunOptions {
	opts.Source = strings.TrimSpace(opts.Source)
	opts.ChecklistPath = strings.TrimSpace(opts.ChecklistPath)
	opts.CronPath = strings.TrimSpace(opts.CronPath)
	opts.ChatInfoContactsDir = strings.TrimSpace(opts.ChatInfoContactsDir)
	if opts.Interval <= 0 {
		opts.Interval = configdefaults.DefaultHeartbeatInterval
	}
	if opts.TaskTimeout <= 0 {
		opts.TaskTimeout = configdefaults.DefaultTaskTimeout
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
	if opts.Source == "" {
		opts.Source = "awareness"
	}
	if opts.ChecklistPath == "" {
		opts.ChecklistPath = paths.HeartbeatPath
	}
	if opts.CronPath == "" {
		opts.CronPath = paths.CronPath
	}
	if opts.ChatInfoContactsDir == "" {
		opts.ChatInfoContactsDir = paths.ContactsDir
	}
	return opts
}
