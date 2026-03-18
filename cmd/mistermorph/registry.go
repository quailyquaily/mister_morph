package main

import (
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/secrets"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
	"github.com/spf13/viper"
)

type registryConfig struct {
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

func applyRegistryViperDefaults() {
	viper.SetDefault("tools.read_file.max_bytes", 256*1024)
	viper.SetDefault("tools.read_file.deny_paths", []string{"config.yaml"})

	viper.SetDefault("tools.write_file.enabled", true)
	viper.SetDefault("tools.write_file.max_bytes", 512*1024)

	viper.SetDefault("tools.bash.enabled", true)
	viper.SetDefault("tools.bash.timeout", 30*time.Second)
	viper.SetDefault("tools.bash.max_output_bytes", 256*1024)
	viper.SetDefault("tools.bash.deny_paths", []string{"config.yaml"})
	viper.SetDefault("tools.bash.injected_env_vars", []string{})

	viper.SetDefault("tools.url_fetch.enabled", true)
	viper.SetDefault("tools.url_fetch.timeout", 30*time.Second)
	viper.SetDefault("tools.url_fetch.max_bytes", int64(512*1024))
	viper.SetDefault("tools.url_fetch.max_bytes_download", int64(100*1024*1024))
	viper.SetDefault("tools.web_search.enabled", true)
	viper.SetDefault("tools.web_search.timeout", 20*time.Second)
	viper.SetDefault("tools.web_search.max_results", 5)
	viper.SetDefault("tools.web_search.base_url", "https://duckduckgo.com/html/")
	viper.SetDefault("tools.contacts_send.enabled", true)
	viper.SetDefault("tools.todo_update.enabled", true)
}

func loadRegistryConfigFromViper() registryConfig {
	applyRegistryViperDefaults()

	authProfiles := map[string]secrets.AuthProfile{}
	_ = viper.UnmarshalKey("auth_profiles", &authProfiles)
	for id, profile := range authProfiles {
		profile.ID = id
		authProfiles[id] = profile
	}

	fileStateDir := strings.TrimSpace(viper.GetString("file_state_dir"))

	return registryConfig{
		UserAgent:                     strings.TrimSpace(viper.GetString("user_agent")),
		SecretsAllowProfiles:          append([]string(nil), viper.GetStringSlice("secrets.allow_profiles")...),
		AuthProfiles:                  copyAuthProfilesMap(authProfiles),
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
		ContactsFailureCooldown:       contactsFailureCooldownFromViper(),
	}
}

func buildRegistryFromConfig(cfg registryConfig, log *slog.Logger) *tools.Registry {
	r := tools.NewRegistry()
	if log == nil {
		log = slog.Default()
	}

	userAgent := strings.TrimSpace(cfg.UserAgent)

	allowProfiles := make(map[string]bool)
	for _, id := range cfg.SecretsAllowProfiles {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		allowProfiles[id] = true
	}

	authProfiles := copyAuthProfilesMap(cfg.AuthProfiles)

	for _, p := range authProfiles {
		if err := p.Validate(); err != nil {
			log.Warn("auth_profile_invalid", "profile", p.ID, "err", err)
			delete(authProfiles, p.ID)
		}
	}

	log.Info("auth_profiles_configured",
		"allow_profiles", keysSorted(allowProfiles),
		"auth_profiles", len(authProfiles),
	)

	profileStore := secrets.NewProfileStore(authProfiles)
	authenticatedHTTPConfigured := hasAllowedAuthProfiles(allowProfiles, authProfiles)

	toolsutil.RegisterStaticTools(r, toolsutil.StaticRegistryConfig{
		Common: toolsutil.StaticCommonConfig{
			UserAgent:                   userAgent,
			FileCacheDir:                strings.TrimSpace(cfg.FileCacheDir),
			FileStateDir:                strings.TrimSpace(cfg.FileStateDir),
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
			TelegramBotToken: strings.TrimSpace(cfg.TelegramBotToken),
			TelegramBaseURL:  strings.TrimSpace(cfg.TelegramBaseURL),
			SlackBotToken:    strings.TrimSpace(cfg.SlackBotToken),
			SlackBaseURL:     strings.TrimSpace(cfg.SlackBaseURL),
			LineChannelToken: strings.TrimSpace(cfg.LineChannelAccessToken),
			LineBaseURL:      strings.TrimSpace(cfg.LineBaseURL),
			LarkAppID:        strings.TrimSpace(cfg.LarkAppID),
			LarkAppSecret:    strings.TrimSpace(cfg.LarkAppSecret),
			LarkBaseURL:      strings.TrimSpace(cfg.LarkBaseURL),
			FailureCooldown:  cfg.ContactsFailureCooldown,
		},
	}, nil)

	return r
}

func contactsFailureCooldownFromViper() time.Duration {
	if viper.IsSet("contacts.proactive.failure_cooldown") {
		if v := viper.GetDuration("contacts.proactive.failure_cooldown"); v > 0 {
			return v
		}
	}
	return 72 * time.Hour
}

func keysSorted(m map[string]bool) []string {
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

func hasAllowedAuthProfiles(allowProfiles map[string]bool, authProfiles map[string]secrets.AuthProfile) bool {
	for id := range allowProfiles {
		if _, ok := authProfiles[id]; ok {
			return true
		}
	}
	return false
}
