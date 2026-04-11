package integration

import (
	"log/slog"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/secrets"
)

type runtimeSnapshot struct {
	Logger            *slog.Logger
	LoggerInitErr     error
	LogOptions        agent.LogOptions
	LLMValues         llmutil.RuntimeValues
	LLMRequestTimeout time.Duration
	AgentLimits       agent.Limits
	SkillsConfig      skillsutil.SkillsConfig
	Registry          registrySnapshot
	Guard             guardSnapshot
	Telegram          channelopts.TelegramConfig
	Slack             channelopts.SlackConfig
	MCPServers        []mcphost.ServerConfig
	ACPAgents         []acpclient.AgentConfig
}

type registrySnapshot struct {
	UserAgent                     string
	SecretsAllowProfiles          []string
	AuthProfiles                  map[string]secrets.AuthProfile
	FileCacheDir                  string
	FileStateDir                  string
	ToolsReadFileMaxBytes         int64
	ToolsReadFileDenyPaths        []string
	ToolsWriteFileEnabled         bool
	ToolsWriteFileMaxBytes        int
	ToolsSpawnEnabled             bool
	ToolsACPSpawnEnabled          bool
	ToolsBashEnabled              bool
	ToolsBashTimeout              time.Duration
	ToolsBashMaxOutputBytes       int
	ToolsBashDenyPaths            []string
	ToolsBashInjectedEnvVars      []string
	ToolsURLFetchEnabled          bool
	ToolsURLFetchTimeout          time.Duration
	ToolsURLFetchMaxBytes         int64
	ToolsURLFetchMaxBytesDownload int64
	ToolsWebSearchEnabled         bool
	ToolsWebSearchTimeout         time.Duration
	ToolsWebSearchMaxResults      int
	ToolsWebSearchBaseURL         string
	ToolsContactsSendEnabled      bool
	ToolsPlanCreateEnabled        bool
	ToolsPlanCreateMaxSteps       int
	ToolsTodoUpdateEnabled        bool
	TODOPathWIP                   string
	TODOPathDone                  string
	ContactsDir                   string
	TelegramBotToken              string
	TelegramBaseURL               string
	SlackBotToken                 string
	SlackBaseURL                  string
	LineChannelAccessToken        string
	LineBaseURL                   string
	ContactsFailureCooldown       time.Duration
}

type guardSnapshot struct {
	Enabled bool
	Config  guard.Config
	Dir     string
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

func cloneACPAgents(in []acpclient.AgentConfig) []acpclient.AgentConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]acpclient.AgentConfig, 0, len(in))
	for _, cfg := range in {
		item := cfg
		item.Args = append([]string(nil), cfg.Args...)
		item.ReadRoots = append([]string(nil), cfg.ReadRoots...)
		item.WriteRoots = append([]string(nil), cfg.WriteRoots...)
		if len(cfg.Env) > 0 {
			item.Env = make(map[string]string, len(cfg.Env))
			for k, v := range cfg.Env {
				item.Env[k] = v
			}
		}
		if len(cfg.SessionOptions) > 0 {
			item.SessionOptions = make(map[string]any, len(cfg.SessionOptions))
			for k, v := range cfg.SessionOptions {
				item.SessionOptions[k] = v
			}
		}
		out = append(out, item)
	}
	return out
}

func copyAuthProfilesMap(in map[string]secrets.AuthProfile) map[string]secrets.AuthProfile {
	if len(in) == 0 {
		return map[string]secrets.AuthProfile{}
	}
	out := make(map[string]secrets.AuthProfile, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
