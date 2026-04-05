package line

import (
	"context"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
)

// Hooks is intentionally minimal in the bootstrap phase.
// Runtime callback shapes will be finalized with line runtime implementation.
type Hooks struct{}

type RunOptions struct {
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

func Run(ctx context.Context, d Dependencies, opts RunOptions) error {
	return runLineLoop(ctx, d, resolveRuntimeLoopOptionsFromRunOptions(opts))
}
