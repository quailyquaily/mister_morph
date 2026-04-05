package telegram

import (
	"context"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

type ServerOptions struct {
	Listen    string
	AuthToken string
	MaxQueue  int
	Poke      daemonruntime.PokeFunc
}

type RunOptions struct {
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
	Hooks                         Hooks
	InspectPrompt                 bool
	InspectRequest                bool
	TaskStore                     daemonruntime.TaskView
}

func Run(ctx context.Context, d Dependencies, opts RunOptions) error {
	return runTelegramLoop(ctx, d, resolveRuntimeLoopOptionsFromRunOptions(opts))
}

func normalizeAllowedChatIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return []int64{}
	}
	out := make([]int64, 0, len(ids))
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return []int64{}
	}
	return out
}
