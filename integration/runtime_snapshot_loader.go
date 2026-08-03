package integration

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/spf13/viper"
)

func loadRuntimeSnapshot(cfg Config) runtimeSnapshot {
	v := viper.New()
	ApplyViperDefaults(v)
	for k, value := range cfg.Overrides {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		v.Set(key, value)
	}
	return loadRuntimeSnapshotFromReader(v)
}

func loadRuntimeSnapshotFromReader(v *viper.Viper) runtimeSnapshot {
	if v == nil {
		v = viper.New()
		ApplyViperDefaults(v)
	}

	paths := runtimepaths.FromReader(v)

	logger, loggerErr := logutil.LoggerFromConfig(logutil.LoggerConfigFromReader(v))
	if loggerErr != nil {
		logger = slog.Default()
	}
	logOpts := cloneLogOptions(logutil.LogOptionsFromConfig(logutil.LogOptionsConfigFromReader(v)))
	llmValues, llmErr := llmutil.RuntimeValuesFromReader(v)
	staticRegistry, registryErr := toolsutil.StaticRegistryConfigFromReader(v)
	guardConfig, guardErr := guard.SnapshotFromReader(v)
	defaultWorkspaceDir, workspaceErr := workspace.ValidateDefaultDir(v.GetString("workspace_dir"))

	return runtimeSnapshot{
		Logger:            logger,
		InitErr:           errors.Join(loggerErr, llmErr, registryErr, guardErr, workspaceErr),
		LogOptions:        logOpts,
		LLMValues:         llmValues,
		LLMRequestTimeout: v.GetDuration("llm.request_timeout"),
		AgentLimits: agent.Limits{
			MaxSteps:        v.GetInt("max_steps"),
			ParseRetries:    v.GetInt("parse_retries"),
			MaxTokenBudget:  v.GetInt("max_token_budget"),
			ToolRepeatLimit: v.GetInt("tool_repeat_limit"),
			ContextCompaction: agent.NewContextCompactionConfig(
				v.GetBool("context_compaction.enabled"),
				v.GetFloat64("context_compaction.trigger_ratio"),
				v.GetInt("context_compaction.output_reserve_tokens"),
			),
		},
		SkillsConfig:   cloneSkillsConfig(skillsutil.SkillsConfigFromReader(v)),
		StaticRegistry: staticRegistry,
		Registry: registrySnapshot{
			ToolsSpawnEnabled:         v.GetBool("tools.spawn.enabled"),
			ToolsACPSpawnEnabled:      v.GetBool("tools.acp_spawn.enabled"),
			ToolsCoderEnabled:         v.GetBool("tools.coder.enabled"),
			ToolsCoderPathExtra:       append([]string(nil), v.GetStringSlice("tools.coder.path_extra")...),
			ToolsPlanCreateEnabled:    v.GetBool("tools.plan_create.enabled"),
			ToolsPlanCreateMaxSteps:   v.GetInt("tools.plan_create.max_steps"),
			ToolsTodoUpdateEnabled:    v.GetBool("tools.todo_update.enabled"),
			ToolsImageGenerateEnabled: v.GetBool("tools.image_generate.enabled"),
			ToolsImageEditEnabled:     v.GetBool("tools.image_edit.enabled"),
			TaskPersistenceTargets:    append([]string(nil), v.GetStringSlice("tasks.persistence_targets")...),
			TasksRotateMaxBytes:       v.GetInt64("tasks.rotate_max_bytes"),
		},
		Guard:               guardConfig,
		Telegram:            channelopts.TelegramConfigFromReader(v),
		Slack:               channelopts.SlackConfigFromReader(v),
		MCPServers:          mcphost.MCPConfigFromReader(v),
		ACPAgents:           acpclient.AgentsFromReader(v),
		Paths:               paths,
		DefaultWorkspaceDir: defaultWorkspaceDir,
		AgentSettings:       agentsettings.NewReaderSnapshot(v),
	}
}
