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
	"github.com/quailyquaily/mistermorph/internal/secref"
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

func ApplyRuntimeConfig(d CommonDependencies, toolsConfig toolsutil.RuntimeToolsRegisterConfig, reader agentsettings.Reader) CommonDependencies {
	d.RuntimeToolsConfig = toolsConfig
	d.RuntimePaths = runtimepaths.FromReader(reader)
	d.DefaultWorkspaceDir = strings.TrimSpace(reader.GetString("workspace_dir"))
	settingsOwner := agentsettings.NewFileOwner(agentsettings.FileOwnerOptions{Reader: reader, OSStore: secref.NewOSStore()})
	d.AgentSettingsOwner = settingsOwner
	d.RuntimeConfigSource = settingsOwner
	d.AgentSettingsReader = settingsOwner.CurrentReader()
	d.TaskPersistenceTargets = append([]string(nil), reader.GetStringSlice("tasks.persistence_targets")...)
	d.TaskRotateMaxBytes = reader.GetInt64("tasks.rotate_max_bytes")
	return d
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
	if err == nil {
		return "unknown error"
	}
	if display := outputfmt.FormatErrorForDisplay(err); display != "" {
		return display
	}
	return "unknown error"
}
