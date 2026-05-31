package lark

import (
	"context"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
)

type HandleModelCommandFunc func(text string) (string, bool, error)
type HandleSkillCommandFunc func(currentLoaded []string) (string, error)

type Dependencies struct {
	depsutil.CommonDependencies
	HandleModelCommand HandleModelCommandFunc
	HandleSkillCommand HandleSkillCommandFunc
}

// Hooks is intentionally minimal in the bootstrap phase.
type Hooks struct{}

type RunOptions struct {
	AppID                         string
	AppSecret                     string
	AllowedChatIDs                []string
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
	BusMaxInFlight                int
	RequestTimeout                time.Duration
	AgentLimits                   agent.Limits
	EngineToolsConfig             agent.EngineToolsConfig
	MemoryEnabled                 bool
	MemoryShortTermDays           int
	MemoryInjectionEnabled        bool
	MemoryInjectionMaxItems       int
	Hooks                         Hooks
	InspectPrompt                 bool
	InspectRequest                bool
}

func Run(ctx context.Context, d Dependencies, opts RunOptions) error {
	return runLarkLoop(ctx, d, resolveRuntimeLoopOptionsFromRunOptions(opts))
}
