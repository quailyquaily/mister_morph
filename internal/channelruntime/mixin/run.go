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
	KeystoreFile           string
	Credentials            mixinapi.Credentials
	AllowedConversationIDs []string
	TaskTimeout            time.Duration
	MaxConcurrency         int
	FileCacheDir           string
	ServerListen           string
	ServerAuthToken        string
	ServerMaxQueue         int
	BusMaxInFlight         int
	AgentLimits            agent.Limits
	EngineToolsConfig      agent.EngineToolsConfig
	InspectPrompt          bool
	InspectRequest         bool
	TaskStore              daemonruntime.TaskView
	OnConnectionChange     func(bool)

	api   mixinAPI
	blaze mixinBlaze
}

func Run(ctx context.Context, d Dependencies, opts RunOptions) error {
	if err := d.CommonDependencies.Validate(); err != nil {
		return err
	}
	return runMixinLoop(ctx, d, normalizeRunOptions(opts))
}
