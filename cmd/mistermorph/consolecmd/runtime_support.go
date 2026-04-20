package consolecmd

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/secrets"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
	"github.com/spf13/viper"
)

type consoleRegistryConfig struct {
	UserAgent                     string
	SecretsAllowProfiles          []string
	AuthProfiles                  map[string]secrets.AuthProfile
	FileCacheDir                  string
	FileStateDir                  string
	ToolsReadFileMaxBytes         int64
	ToolsReadFileDenyPaths        []string
	ToolsWriteFileEnabled         bool
	ToolsWriteFileMaxBytes        int
	ToolsBashEnabled              bool
	ToolsBashTimeout              time.Duration
	ToolsBashMaxOutputBytes       int
	ToolsBashDenyPaths            []string
	ToolsBashInjectedEnvVars      []string
	ToolsPowerShellEnabled        bool
	ToolsPowerShellTimeout        time.Duration
	ToolsPowerShellMaxOutputBytes int
	ToolsPowerShellDenyPaths      []string
	ToolsPowerShellInjectedEnvVars []string
	ToolsURLFetchEnabled          bool
	ToolsURLFetchTimeout          time.Duration
	ToolsURLFetchMaxBytes         int64
	ToolsURLFetchMaxBytesDownload int64
	ToolsWebSearchEnabled         bool
	ToolsWebSearchTimeout         time.Duration
	ToolsWebSearchMaxResults      int
	ToolsWebSearchBaseURL         string
	ToolsContactsSendEnabled      bool
	ContactsDir                   string
	TelegramBotToken              string
	TelegramBaseURL               string
	SlackBotToken                 string
	SlackBaseURL                  string
	LineChannelAccessToken        string
	LineBaseURL                   string
	LarkAppID                     string
	LarkAppSecret                 string
	LarkBaseURL                   string
	ContactsFailureCooldown       time.Duration
}

func loadConsoleRegistryConfigFromViper() consoleRegistryConfig {
	return loadConsoleRegistryConfigFromReader(viper.GetViper())
}

func loadConsoleRegistryConfigFromReader(r *viper.Viper) consoleRegistryConfig {
	if r == nil {
		return consoleRegistryConfig{}
	}
	authProfiles := map[string]secrets.AuthProfile{}
	_ = r.UnmarshalKey("auth_profiles", &authProfiles)
	for id, profile := range authProfiles {
		profile.ID = id
		authProfiles[id] = profile
	}

	fileStateDir := strings.TrimSpace(r.GetString("file_state_dir"))
	return consoleRegistryConfig{
		UserAgent:                     strings.TrimSpace(r.GetString("user_agent")),
		SecretsAllowProfiles:          append([]string(nil), r.GetStringSlice("secrets.allow_profiles")...),
		AuthProfiles:                  copyConsoleAuthProfilesMap(authProfiles),
		FileCacheDir:                  strings.TrimSpace(r.GetString("file_cache_dir")),
		FileStateDir:                  fileStateDir,
		ToolsReadFileMaxBytes:         int64(r.GetInt("tools.read_file.max_bytes")),
		ToolsReadFileDenyPaths:        append([]string(nil), r.GetStringSlice("tools.read_file.deny_paths")...),
		ToolsWriteFileEnabled:         r.GetBool("tools.write_file.enabled"),
		ToolsWriteFileMaxBytes:        r.GetInt("tools.write_file.max_bytes"),
		ToolsBashEnabled:              r.GetBool("tools.bash.enabled"),
		ToolsBashTimeout:              r.GetDuration("tools.bash.timeout"),
		ToolsBashMaxOutputBytes:       r.GetInt("tools.bash.max_output_bytes"),
		ToolsBashDenyPaths:            append([]string(nil), r.GetStringSlice("tools.bash.deny_paths")...),
		ToolsBashInjectedEnvVars:      append([]string(nil), r.GetStringSlice("tools.bash.injected_env_vars")...),
		ToolsPowerShellEnabled:        r.GetBool("tools.powershell.enabled"),
		ToolsPowerShellTimeout:        r.GetDuration("tools.powershell.timeout"),
		ToolsPowerShellMaxOutputBytes: r.GetInt("tools.powershell.max_output_bytes"),
		ToolsPowerShellDenyPaths:      append([]string(nil), r.GetStringSlice("tools.powershell.deny_paths")...),
		ToolsPowerShellInjectedEnvVars: append([]string(nil), r.GetStringSlice("tools.powershell.injected_env_vars")...),
		ToolsURLFetchEnabled:          r.GetBool("tools.url_fetch.enabled"),
		ToolsURLFetchTimeout:          r.GetDuration("tools.url_fetch.timeout"),
		ToolsURLFetchMaxBytes:         r.GetInt64("tools.url_fetch.max_bytes"),
		ToolsURLFetchMaxBytesDownload: r.GetInt64("tools.url_fetch.max_bytes_download"),
		ToolsWebSearchEnabled:         r.GetBool("tools.web_search.enabled"),
		ToolsWebSearchTimeout:         r.GetDuration("tools.web_search.timeout"),
		ToolsWebSearchMaxResults:      r.GetInt("tools.web_search.max_results"),
		ToolsWebSearchBaseURL:         strings.TrimSpace(r.GetString("tools.web_search.base_url")),
		ToolsContactsSendEnabled:      r.GetBool("tools.contacts_send.enabled"),
		ContactsDir:                   pathutil.ResolveStateChildDir(fileStateDir, strings.TrimSpace(r.GetString("contacts.dir_name")), "contacts"),
		TelegramBotToken:              strings.TrimSpace(r.GetString("telegram.bot_token")),
		TelegramBaseURL:               "https://api.telegram.org",
		SlackBotToken:                 strings.TrimSpace(r.GetString("slack.bot_token")),
		SlackBaseURL:                  strings.TrimSpace(r.GetString("slack.base_url")),
		LineChannelAccessToken:        strings.TrimSpace(r.GetString("line.channel_access_token")),
		LineBaseURL:                   strings.TrimSpace(r.GetString("line.base_url")),
		LarkAppID:                     strings.TrimSpace(r.GetString("lark.app_id")),
		LarkAppSecret:                 strings.TrimSpace(r.GetString("lark.app_secret")),
		LarkBaseURL:                   strings.TrimSpace(r.GetString("lark.base_url")),
		ContactsFailureCooldown:       consoleContactsFailureCooldownFromReader(r),
	}
}

func buildConsoleBaseRegistry(ctx context.Context, logger *slog.Logger) (*tools.Registry, *mcphost.Host) {
	return buildConsoleBaseRegistryFromReader(ctx, logger, viper.GetViper())
}

func buildConsoleBaseRegistryFromReader(ctx context.Context, logger *slog.Logger, r *viper.Viper) (*tools.Registry, *mcphost.Host) {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := loadConsoleRegistryConfigFromReader(r)
	reg := tools.NewRegistry()

	allowProfiles := make(map[string]bool)
	for _, id := range cfg.SecretsAllowProfiles {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		allowProfiles[id] = true
	}
	authProfiles := copyConsoleAuthProfilesMap(cfg.AuthProfiles)
	for id, profile := range authProfiles {
		if err := profile.Validate(); err != nil {
			logger.Warn("auth_profile_invalid", "profile", profile.ID, "err", err)
			delete(authProfiles, id)
		}
	}

	logger.Info("auth_profiles_configured",
		"allow_profiles", consoleSortedKeys(allowProfiles),
		"auth_profiles", len(authProfiles),
	)

	profileStore := secrets.NewProfileStore(authProfiles)
	authenticatedHTTPConfigured := consoleHasAllowedAuthProfiles(allowProfiles, authProfiles)

	toolsutil.RegisterStaticTools(reg, toolsutil.StaticRegistryConfig{
		Common: toolsutil.StaticCommonConfig{
			UserAgent:                   cfg.UserAgent,
			PathRoots:                   pathroots.New("", cfg.FileCacheDir, cfg.FileStateDir),
			AuthenticatedHTTPConfigured: authenticatedHTTPConfigured,
		},
		ReadFile: toolsutil.StaticReadFileConfig{
			MaxBytes:  cfg.ToolsReadFileMaxBytes,
			DenyPaths: append([]string(nil), cfg.ToolsReadFileDenyPaths...),
		},
		WriteFile: toolsutil.StaticWriteFileConfig{
			Enabled:  cfg.ToolsWriteFileEnabled,
			MaxBytes: cfg.ToolsWriteFileMaxBytes,
		},
		Bash: toolsutil.StaticBashConfig{
			Enabled:         cfg.ToolsBashEnabled,
			Timeout:         cfg.ToolsBashTimeout,
			MaxOutputBytes:  cfg.ToolsBashMaxOutputBytes,
			DenyPaths:       append([]string(nil), cfg.ToolsBashDenyPaths...),
			InjectedEnvVars: append([]string(nil), cfg.ToolsBashInjectedEnvVars...),
		},
		PowerShell: toolsutil.StaticPowerShellConfig{
			Enabled:         cfg.ToolsPowerShellEnabled,
			Timeout:         cfg.ToolsPowerShellTimeout,
			MaxOutputBytes:  cfg.ToolsPowerShellMaxOutputBytes,
			DenyPaths:       append([]string(nil), cfg.ToolsPowerShellDenyPaths...),
			InjectedEnvVars: append([]string(nil), cfg.ToolsPowerShellInjectedEnvVars...),
		},
		URLFetch: toolsutil.StaticURLFetchConfig{
			Enabled:          cfg.ToolsURLFetchEnabled,
			Timeout:          cfg.ToolsURLFetchTimeout,
			MaxBytes:         cfg.ToolsURLFetchMaxBytes,
			MaxBytesDownload: cfg.ToolsURLFetchMaxBytesDownload,
			Auth: &builtin.URLFetchAuth{
				AllowProfiles: allowProfiles,
				Profiles:      profileStore,
			},
		},
		WebSearch: toolsutil.StaticWebSearchConfig{
			Enabled:    cfg.ToolsWebSearchEnabled,
			Timeout:    cfg.ToolsWebSearchTimeout,
			MaxResults: cfg.ToolsWebSearchMaxResults,
			BaseURL:    cfg.ToolsWebSearchBaseURL,
		},
		ContactsSend: toolsutil.StaticContactsSendConfig{
			Enabled:          cfg.ToolsContactsSendEnabled,
			ContactsDir:      cfg.ContactsDir,
			TelegramBotToken: cfg.TelegramBotToken,
			TelegramBaseURL:  cfg.TelegramBaseURL,
			SlackBotToken:    cfg.SlackBotToken,
			SlackBaseURL:     cfg.SlackBaseURL,
			LineChannelToken: cfg.LineChannelAccessToken,
			LineBaseURL:      cfg.LineBaseURL,
			LarkAppID:        cfg.LarkAppID,
			LarkAppSecret:    cfg.LarkAppSecret,
			LarkBaseURL:      cfg.LarkBaseURL,
			FailureCooldown:  cfg.ContactsFailureCooldown,
		},
	}, nil)

	host, err := mcphost.RegisterTools(ctx, mcphost.MCPConfigFromReader(r), reg, logger)
	if err != nil {
		logger.Warn("mcp_init_failed", "err", err)
	}
	return reg, host
}

func buildConsoleGuardFromViper(logger *slog.Logger) *guard.Guard {
	return buildConsoleGuardFromReader(logger, viper.GetViper())
}

func buildConsoleGuardFromReader(logger *slog.Logger, r *viper.Viper) *guard.Guard {
	if logger == nil {
		logger = slog.Default()
	}
	if r == nil {
		return nil
	}

	var patterns []guard.RegexPattern
	_ = r.UnmarshalKey("guard.redaction.patterns", &patterns)

	cfg := guard.Config{
		Enabled: true,
		Network: guard.NetworkConfig{
			URLFetch: guard.URLFetchNetworkPolicy{
				AllowedURLPrefixes: append([]string(nil), r.GetStringSlice("guard.network.url_fetch.allowed_url_prefixes")...),
				DenyPrivateIPs:     r.GetBool("guard.network.url_fetch.deny_private_ips"),
				FollowRedirects:    r.GetBool("guard.network.url_fetch.follow_redirects"),
				AllowProxy:         r.GetBool("guard.network.url_fetch.allow_proxy"),
			},
		},
		Redaction: guard.RedactionConfig{
			Enabled:  r.GetBool("guard.redaction.enabled"),
			Patterns: append([]guard.RegexPattern(nil), patterns...),
		},
		Audit: guard.AuditConfig{
			JSONLPath:      strings.TrimSpace(r.GetString("guard.audit.jsonl_path")),
			RotateMaxBytes: r.GetInt64("guard.audit.rotate_max_bytes"),
		},
		Approvals: guard.ApprovalsConfig{
			Enabled: r.GetBool("guard.approvals.enabled"),
		},
	}
	if !r.GetBool("guard.enabled") {
		return nil
	}

	guardDir := resolveConsoleGuardDir(r.GetString("file_state_dir"), r.GetString("guard.dir_name"))
	if err := os.MkdirAll(guardDir, 0o700); err != nil {
		logger.Warn("guard_dir_create_error", "error", err.Error(), "guard_dir", guardDir)
		return nil
	}
	lockRoot := filepath.Join(guardDir, ".fslocks")

	jsonlPath := strings.TrimSpace(cfg.Audit.JSONLPath)
	if jsonlPath == "" {
		jsonlPath = filepath.Join(guardDir, "audit", "guard_audit.jsonl")
	}
	jsonlPath = pathutil.ExpandHomePath(jsonlPath)

	var sink guard.AuditSink
	var warnings []string
	if jsonlPath != "" {
		s, err := guard.NewJSONLAuditSink(jsonlPath, cfg.Audit.RotateMaxBytes, lockRoot)
		if err != nil {
			logger.Warn("guard_audit_sink_error", "error", err.Error())
			warnings = append(warnings, "guard_audit_sink_error: "+err.Error())
		} else {
			sink = s
		}
	}

	var approvals guard.ApprovalStore
	if cfg.Approvals.Enabled {
		approvalsPath := filepath.Join(guardDir, "approvals", "guard_approvals.json")
		st, err := guard.NewFileApprovalStore(approvalsPath, lockRoot)
		if err != nil {
			logger.Warn("guard_approvals_store_error", "error", err.Error())
			warnings = append(warnings, "guard_approvals_store_error: "+err.Error())
		} else {
			approvals = st
		}
	}

	logger.Info("guard_enabled",
		"guard_dir", guardDir,
		"url_fetch_prefixes", len(cfg.Network.URLFetch.AllowedURLPrefixes),
		"audit_jsonl", jsonlPath,
		"approvals_enabled", approvals != nil,
	)

	if len(warnings) > 0 {
		return guard.NewWithWarnings(cfg, sink, approvals, warnings)
	}
	return guard.New(cfg, sink, approvals)
}

func resolveConsoleGuardDir(fileStateDir, guardDirName string) string {
	base := pathutil.ResolveStateDir(fileStateDir)
	home, err := os.UserHomeDir()
	if strings.TrimSpace(base) == "" && err == nil && strings.TrimSpace(home) != "" {
		base = filepath.Join(home, ".morph")
	}
	if strings.TrimSpace(base) == "" {
		base = filepath.Join(".", ".morph")
	}
	name := strings.TrimSpace(guardDirName)
	if name == "" {
		name = "guard"
	}
	return filepath.Join(base, name)
}

func consoleAgentConfigFromViper() agent.Config {
	return consoleAgentConfigFromReader(viper.GetViper())
}

func consoleAgentConfigFromReader(r interface {
	GetInt(string) int
}) agent.Config {
	if r == nil {
		return agent.Config{}
	}
	return agent.Config{
		MaxSteps:        r.GetInt("max_steps"),
		ParseRetries:    r.GetInt("parse_retries"),
		MaxTokenBudget:  r.GetInt("max_token_budget"),
		ToolRepeatLimit: r.GetInt("tool_repeat_limit"),
	}
}

func consoleEngineToolsConfigFromViper() agent.EngineToolsConfig {
	return consoleEngineToolsConfigFromReader(viper.GetViper())
}

func consoleEngineToolsConfigFromReader(r interface {
	GetBool(string) bool
}) agent.EngineToolsConfig {
	if r == nil {
		return agent.EngineToolsConfig{}
	}
	return agent.EngineToolsConfig{
		SpawnEnabled:    r.GetBool("tools.spawn.enabled"),
		ACPSpawnEnabled: r.GetBool("tools.acp_spawn.enabled"),
	}
}

func cloneConsoleRegistry(base *tools.Registry) *tools.Registry {
	reg := tools.NewRegistry()
	if base == nil {
		return reg
	}
	for _, t := range base.All() {
		reg.Register(t)
	}
	return reg
}

func consoleContactsFailureCooldownFromViper() time.Duration {
	return consoleContactsFailureCooldownFromReader(viper.GetViper())
}

func consoleContactsFailureCooldownFromReader(r *viper.Viper) time.Duration {
	if r == nil {
		return 72 * time.Hour
	}
	if r.IsSet("contacts.proactive.failure_cooldown") {
		if v := r.GetDuration("contacts.proactive.failure_cooldown"); v > 0 {
			return v
		}
	}
	return 72 * time.Hour
}

func consoleSortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func copyConsoleAuthProfilesMap(in map[string]secrets.AuthProfile) map[string]secrets.AuthProfile {
	if len(in) == 0 {
		return map[string]secrets.AuthProfile{}
	}
	out := make(map[string]secrets.AuthProfile, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func consoleHasAllowedAuthProfiles(allowProfiles map[string]bool, authProfiles map[string]secrets.AuthProfile) bool {
	for id := range allowProfiles {
		if _, ok := authProfiles[id]; ok {
			return true
		}
	}
	return false
}
