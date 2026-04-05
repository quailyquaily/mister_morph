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
	authProfiles := map[string]secrets.AuthProfile{}
	_ = viper.UnmarshalKey("auth_profiles", &authProfiles)
	for id, profile := range authProfiles {
		profile.ID = id
		authProfiles[id] = profile
	}

	fileStateDir := strings.TrimSpace(viper.GetString("file_state_dir"))
	return consoleRegistryConfig{
		UserAgent:                     strings.TrimSpace(viper.GetString("user_agent")),
		SecretsAllowProfiles:          append([]string(nil), viper.GetStringSlice("secrets.allow_profiles")...),
		AuthProfiles:                  copyConsoleAuthProfilesMap(authProfiles),
		FileCacheDir:                  strings.TrimSpace(viper.GetString("file_cache_dir")),
		FileStateDir:                  fileStateDir,
		ToolsReadFileMaxBytes:         int64(viper.GetInt("tools.read_file.max_bytes")),
		ToolsReadFileDenyPaths:        append([]string(nil), viper.GetStringSlice("tools.read_file.deny_paths")...),
		ToolsWriteFileEnabled:         viper.GetBool("tools.write_file.enabled"),
		ToolsWriteFileMaxBytes:        viper.GetInt("tools.write_file.max_bytes"),
		ToolsBashEnabled:              viper.GetBool("tools.bash.enabled"),
		ToolsBashTimeout:              viper.GetDuration("tools.bash.timeout"),
		ToolsBashMaxOutputBytes:       viper.GetInt("tools.bash.max_output_bytes"),
		ToolsBashDenyPaths:            append([]string(nil), viper.GetStringSlice("tools.bash.deny_paths")...),
		ToolsBashInjectedEnvVars:      append([]string(nil), viper.GetStringSlice("tools.bash.injected_env_vars")...),
		ToolsURLFetchEnabled:          viper.GetBool("tools.url_fetch.enabled"),
		ToolsURLFetchTimeout:          viper.GetDuration("tools.url_fetch.timeout"),
		ToolsURLFetchMaxBytes:         viper.GetInt64("tools.url_fetch.max_bytes"),
		ToolsURLFetchMaxBytesDownload: viper.GetInt64("tools.url_fetch.max_bytes_download"),
		ToolsWebSearchEnabled:         viper.GetBool("tools.web_search.enabled"),
		ToolsWebSearchTimeout:         viper.GetDuration("tools.web_search.timeout"),
		ToolsWebSearchMaxResults:      viper.GetInt("tools.web_search.max_results"),
		ToolsWebSearchBaseURL:         strings.TrimSpace(viper.GetString("tools.web_search.base_url")),
		ToolsContactsSendEnabled:      viper.GetBool("tools.contacts_send.enabled"),
		ContactsDir:                   pathutil.ResolveStateChildDir(fileStateDir, strings.TrimSpace(viper.GetString("contacts.dir_name")), "contacts"),
		TelegramBotToken:              strings.TrimSpace(viper.GetString("telegram.bot_token")),
		TelegramBaseURL:               "https://api.telegram.org",
		SlackBotToken:                 strings.TrimSpace(viper.GetString("slack.bot_token")),
		SlackBaseURL:                  strings.TrimSpace(viper.GetString("slack.base_url")),
		LineChannelAccessToken:        strings.TrimSpace(viper.GetString("line.channel_access_token")),
		LineBaseURL:                   strings.TrimSpace(viper.GetString("line.base_url")),
		LarkAppID:                     strings.TrimSpace(viper.GetString("lark.app_id")),
		LarkAppSecret:                 strings.TrimSpace(viper.GetString("lark.app_secret")),
		LarkBaseURL:                   strings.TrimSpace(viper.GetString("lark.base_url")),
		ContactsFailureCooldown:       consoleContactsFailureCooldownFromViper(),
	}
}

func buildConsoleBaseRegistry(ctx context.Context, logger *slog.Logger) (*tools.Registry, *mcphost.Host) {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := loadConsoleRegistryConfigFromViper()
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
			FileCacheDir:                cfg.FileCacheDir,
			FileStateDir:                cfg.FileStateDir,
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

	host, err := mcphost.RegisterTools(ctx, mcphost.MCPConfigFromViper(), reg, logger)
	if err != nil {
		logger.Warn("mcp_init_failed", "err", err)
	}
	return reg, host
}

func buildConsoleGuardFromViper(logger *slog.Logger) *guard.Guard {
	if logger == nil {
		logger = slog.Default()
	}

	var patterns []guard.RegexPattern
	_ = viper.UnmarshalKey("guard.redaction.patterns", &patterns)

	cfg := guard.Config{
		Enabled: true,
		Network: guard.NetworkConfig{
			URLFetch: guard.URLFetchNetworkPolicy{
				AllowedURLPrefixes: append([]string(nil), viper.GetStringSlice("guard.network.url_fetch.allowed_url_prefixes")...),
				DenyPrivateIPs:     viper.GetBool("guard.network.url_fetch.deny_private_ips"),
				FollowRedirects:    viper.GetBool("guard.network.url_fetch.follow_redirects"),
				AllowProxy:         viper.GetBool("guard.network.url_fetch.allow_proxy"),
			},
		},
		Redaction: guard.RedactionConfig{
			Enabled:  viper.GetBool("guard.redaction.enabled"),
			Patterns: append([]guard.RegexPattern(nil), patterns...),
		},
		Audit: guard.AuditConfig{
			JSONLPath:      strings.TrimSpace(viper.GetString("guard.audit.jsonl_path")),
			RotateMaxBytes: viper.GetInt64("guard.audit.rotate_max_bytes"),
		},
		Approvals: guard.ApprovalsConfig{
			Enabled: viper.GetBool("guard.approvals.enabled"),
		},
	}
	if !viper.GetBool("guard.enabled") {
		return nil
	}

	guardDir := resolveConsoleGuardDir(viper.GetString("file_state_dir"), viper.GetString("guard.dir_name"))
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
	return agent.Config{
		MaxSteps:        viper.GetInt("max_steps"),
		ParseRetries:    viper.GetInt("parse_retries"),
		MaxTokenBudget:  viper.GetInt("max_token_budget"),
		ToolRepeatLimit: viper.GetInt("tool_repeat_limit"),
	}
}

func consoleEngineToolsConfigFromViper() agent.EngineToolsConfig {
	return agent.EngineToolsConfig{
		SpawnEnabled: viper.GetBool("tools.spawn.enabled"),
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
	if viper.IsSet("contacts.proactive.failure_cooldown") {
		if v := viper.GetDuration("contacts.proactive.failure_cooldown"); v > 0 {
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
