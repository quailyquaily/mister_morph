package depsutil

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type PromptSpecFunc func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error)

type CommonDependencies struct {
	Logger                       func() (*slog.Logger, error)
	LogOptions                   func() agent.LogOptions
	ResolveLLMRoute              func(purpose string) (llmutil.ResolvedRoute, error)
	ResolveLLMRouteWithProfile   func(purpose, profile string) (llmutil.ResolvedRoute, error)
	CreateLLMClient              func(route llmutil.ResolvedRoute) (llm.Client, error)
	CreateImageClient            func() (llm.ImageClient, error)
	Registry                     func() *tools.Registry
	AwarenessRegistry            func() *tools.Registry
	ToolTriggers                 func(task string) map[string]bool
	RegisterTriggeredStaticTools func(*tools.Registry, map[string]bool)
	ACPAgents                    func() []acpclient.AgentConfig
	RuntimeToolsConfig           toolsutil.RuntimeToolsRegisterConfig
	RuntimePaths                 runtimepaths.Paths
	DefaultWorkspaceDir          string
	AgentSettingsOwner           agentsettings.Owner
	RuntimeConfigSource          agentsettings.RuntimeConfigSource
	AgentSettingsReader          agentsettings.Reader
	TaskPersistenceTargets       []string
	TaskRotateMaxBytes           int64
	// Guard returns a new caller-owned guard. The caller must close a non-nil result.
	Guard         func(logger *slog.Logger) (*guard.Guard, error)
	PromptSpec    PromptSpecFunc
	PromptAugment func(spec *agent.PromptSpec, reg *tools.Registry)
}

func (d CommonDependencies) Validate() error {
	switch {
	case d.Logger == nil:
		return fmt.Errorf("missing required dependency: Logger")
	case d.ResolveLLMRoute == nil:
		return fmt.Errorf("missing required dependency: ResolveLLMRoute")
	case d.CreateLLMClient == nil:
		return fmt.Errorf("missing required dependency: CreateLLMClient")
	case d.PromptSpec == nil:
		return fmt.Errorf("missing required dependency: PromptSpec")
	default:
		return nil
	}
}

func FormatRuntimeError(err error) string {
	s := strings.TrimSpace(outputfmt.FormatErrorForDisplay(err))
	if s != "" {
		return s
	}
	if err == nil {
		return "unknown error"
	}
	raw := strings.TrimSpace(err.Error())
	if raw == "" {
		return "unknown error"
	}
	return raw
}
