package integration

import (
	"log/slog"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
)

type runtimeSnapshot struct {
	Logger              *slog.Logger
	InitErr             error
	LogOptions          agent.LogOptions
	LLMValues           llmutil.RuntimeValues
	LLMRequestTimeout   time.Duration
	AgentLimits         agent.Limits
	SkillsConfig        skillsutil.SkillsConfig
	StaticRegistry      toolsutil.StaticRegistryConfig
	Registry            registrySnapshot
	Guard               guard.Snapshot
	Telegram            channelopts.TelegramConfig
	Slack               channelopts.SlackConfig
	Mixin               channelopts.MixinConfig
	MCPServers          []mcphost.ServerConfig
	ACPAgents           []acpclient.AgentConfig
	Paths               runtimepaths.Paths
	DefaultWorkspaceDir string
	AgentSettings       *agentsettings.ReaderSnapshot
}

type registrySnapshot struct {
	ToolsSpawnEnabled         bool
	ToolsACPSpawnEnabled      bool
	ToolsCoderEnabled         bool
	ToolsCoderPathExtra       []string
	ToolsPlanCreateEnabled    bool
	ToolsPlanCreateMaxSteps   int
	ToolsTodoUpdateEnabled    bool
	ToolsImageGenerateEnabled bool
	ToolsImageEditEnabled     bool
	TaskPersistenceTargets    []string
	TasksRotateMaxBytes       int64
}

func cloneLogOptions(in agent.LogOptions) agent.LogOptions {
	out := in
	out.RedactKeys = append([]string(nil), in.RedactKeys...)
	return out
}

func cloneSkillsConfig(in skillsutil.SkillsConfig) skillsutil.SkillsConfig {
	out := in
	out.Roots = append([]string(nil), in.Roots...)
	out.Requested = append([]string(nil), in.Requested...)
	return out
}
