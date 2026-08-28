package mixin

import (
	"context"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

type HandleModelCommandFunc func(text string) (string, bool, error)
type HandleSkillCommandFunc func(currentLoaded []string) (string, error)

type Dependencies struct {
	depsutil.CommonDependencies
	HandleModelCommand HandleModelCommandFunc
	HandleSkillCommand HandleSkillCommandFunc
}

type RunOptions struct {
	KeystoreFile                  string
	Credentials                   mixinapi.Credentials
	AllowedConversationIDs        []string
	GroupTriggerMode              string
	RecordUntriggered             bool
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	FileCacheDir                  string
	ServerListen                  string
	ServerAuthToken               string
	ServerMaxQueue                int
	BusMaxInFlight                int
	RequestTimeout                time.Duration
	AgentLimits                   agent.Limits
	EngineToolsConfig             agent.EngineToolsConfig
	InspectPrompt                 bool
	InspectRequest                bool
	TaskStore                     daemonruntime.TaskView

	api   mixinAPI
	blaze mixinBlaze
}

func Run(ctx context.Context, d Dependencies, opts RunOptions) error {
	if err := d.CommonDependencies.Validate(); err != nil {
		return err
	}
	return runMixinLoop(ctx, d, normalizeRunOptions(opts))
}
