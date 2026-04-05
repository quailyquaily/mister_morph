package integration

import (
	"log/slog"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/secrets"
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

	fileStateDir := strings.TrimSpace(v.GetString("file_state_dir"))

	authProfiles := map[string]secrets.AuthProfile{}
	_ = v.UnmarshalKey("auth_profiles", &authProfiles)
	for id, profile := range authProfiles {
		profile.ID = id
		authProfiles[id] = profile
	}

	var guardPatterns []guard.RegexPattern
	_ = v.UnmarshalKey("guard.redaction.patterns", &guardPatterns)

	logger, loggerErr := logutil.LoggerFromConfig(logutil.LoggerConfigFromReader(v))
	if loggerErr != nil {
		logger = slog.Default()
	}
	logOpts := cloneLogOptions(logutil.LogOptionsFromConfig(logutil.LogOptionsConfigFromReader(v)))
	llmValues := llmutil.RuntimeValuesFromReader(v)

	return runtimeSnapshot{
		Logger:            logger,
		LoggerInitErr:     loggerErr,
		LogOptions:        logOpts,
		LLMValues:         llmValues,
		LLMRequestTimeout: v.GetDuration("llm.request_timeout"),
		AgentLimits: agent.Limits{
			MaxSteps:        v.GetInt("max_steps"),
			ParseRetries:    v.GetInt("parse_retries"),
			MaxTokenBudget:  v.GetInt("max_token_budget"),
			ToolRepeatLimit: v.GetInt("tool_repeat_limit"),
		},
		SkillsConfig: cloneSkillsConfig(skillsutil.SkillsConfigFromReader(v)),
		Registry: registrySnapshot{
			UserAgent:                     strings.TrimSpace(v.GetString("user_agent")),
			SecretsAllowProfiles:          append([]string(nil), v.GetStringSlice("secrets.allow_profiles")...),
			AuthProfiles:                  copyAuthProfilesMap(authProfiles),
			FileCacheDir:                  strings.TrimSpace(v.GetString("file_cache_dir")),
			FileStateDir:                  fileStateDir,
			ToolsReadFileMaxBytes:         int64(v.GetInt("tools.read_file.max_bytes")),
			ToolsReadFileDenyPaths:        append([]string(nil), v.GetStringSlice("tools.read_file.deny_paths")...),
			ToolsWriteFileEnabled:         v.GetBool("tools.write_file.enabled"),
			ToolsWriteFileMaxBytes:        v.GetInt("tools.write_file.max_bytes"),
			ToolsSpawnEnabled:             v.GetBool("tools.spawn.enabled"),
			ToolsBashEnabled:              v.GetBool("tools.bash.enabled"),
			ToolsBashTimeout:              v.GetDuration("tools.bash.timeout"),
			ToolsBashMaxOutputBytes:       v.GetInt("tools.bash.max_output_bytes"),
			ToolsBashDenyPaths:            append([]string(nil), v.GetStringSlice("tools.bash.deny_paths")...),
			ToolsBashInjectedEnvVars:      append([]string(nil), v.GetStringSlice("tools.bash.injected_env_vars")...),
			ToolsURLFetchEnabled:          v.GetBool("tools.url_fetch.enabled"),
			ToolsURLFetchTimeout:          v.GetDuration("tools.url_fetch.timeout"),
			ToolsURLFetchMaxBytes:         v.GetInt64("tools.url_fetch.max_bytes"),
			ToolsURLFetchMaxBytesDownload: v.GetInt64("tools.url_fetch.max_bytes_download"),
			ToolsWebSearchEnabled:         v.GetBool("tools.web_search.enabled"),
			ToolsWebSearchTimeout:         v.GetDuration("tools.web_search.timeout"),
			ToolsWebSearchMaxResults:      v.GetInt("tools.web_search.max_results"),
			ToolsWebSearchBaseURL:         v.GetString("tools.web_search.base_url"),
			ToolsContactsSendEnabled:      v.GetBool("tools.contacts_send.enabled"),
			ToolsPlanCreateEnabled:        v.GetBool("tools.plan_create.enabled"),
			ToolsPlanCreateMaxSteps:       v.GetInt("tools.plan_create.max_steps"),
			ToolsTodoUpdateEnabled:        v.GetBool("tools.todo_update.enabled"),
			TODOPathWIP:                   pathutil.ResolveStateFile(fileStateDir, statepaths.TODOWIPFilename),
			TODOPathDone:                  pathutil.ResolveStateFile(fileStateDir, statepaths.TODODONEFilename),
			ContactsDir:                   pathutil.ResolveStateChildDir(fileStateDir, strings.TrimSpace(v.GetString("contacts.dir_name")), "contacts"),
			TelegramBotToken:              strings.TrimSpace(v.GetString("telegram.bot_token")),
			TelegramBaseURL:               "https://api.telegram.org",
			SlackBotToken:                 strings.TrimSpace(v.GetString("slack.bot_token")),
			SlackBaseURL:                  strings.TrimSpace(v.GetString("slack.base_url")),
			LineChannelAccessToken:        strings.TrimSpace(v.GetString("line.channel_access_token")),
			LineBaseURL:                   strings.TrimSpace(v.GetString("line.base_url")),
			ContactsFailureCooldown:       contactsFailureCooldownFromReader(v),
		},
		Guard: guardSnapshot{
			Enabled: v.GetBool("guard.enabled"),
			Config: guard.Config{
				Enabled: true,
				Network: guard.NetworkConfig{
					URLFetch: guard.URLFetchNetworkPolicy{
						AllowedURLPrefixes: append([]string(nil), v.GetStringSlice("guard.network.url_fetch.allowed_url_prefixes")...),
						DenyPrivateIPs:     v.GetBool("guard.network.url_fetch.deny_private_ips"),
						FollowRedirects:    v.GetBool("guard.network.url_fetch.follow_redirects"),
						AllowProxy:         v.GetBool("guard.network.url_fetch.allow_proxy"),
					},
				},
				Redaction: guard.RedactionConfig{
					Enabled:  v.GetBool("guard.redaction.enabled"),
					Patterns: append([]guard.RegexPattern(nil), guardPatterns...),
				},
				Audit: guard.AuditConfig{
					JSONLPath:      strings.TrimSpace(v.GetString("guard.audit.jsonl_path")),
					RotateMaxBytes: v.GetInt64("guard.audit.rotate_max_bytes"),
				},
				Approvals: guard.ApprovalsConfig{
					Enabled: v.GetBool("guard.approvals.enabled"),
				},
			},
			Dir: pathutil.ResolveStateChildDir(fileStateDir, strings.TrimSpace(v.GetString("guard.dir_name")), "guard"),
		},
		Telegram:   channelopts.TelegramConfigFromReader(v),
		Slack:      channelopts.SlackConfigFromReader(v),
		MCPServers: mcphost.MCPConfigFromReader(v),
	}
}

func contactsFailureCooldownFromReader(v *viper.Viper) time.Duration {
	if v == nil {
		return 72 * time.Hour
	}
	if v.IsSet("contacts.proactive.failure_cooldown") {
		if value := v.GetDuration("contacts.proactive.failure_cooldown"); value > 0 {
			return value
		}
	}
	return 72 * time.Hour
}
