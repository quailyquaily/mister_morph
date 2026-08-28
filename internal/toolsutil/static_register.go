package toolsutil

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/caprefs"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/shellenv"
	"github.com/quailyquaily/mistermorph/secrets"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
	"github.com/spf13/viper"
)

const (
	BuiltinReadFile      = "read_file"
	BuiltinWriteFile     = "write_file"
	BuiltinBash          = "bash"
	BuiltinPowerShell    = "powershell"
	BuiltinURLFetch      = "url_fetch"
	BuiltinWebSearch     = "web_search"
	BuiltinPlanCreate    = "plan_create"
	BuiltinTodoUpdate    = "todo_update"
	BuiltinImageGenerate = "image_generate"
	BuiltinImageEdit     = "image_edit"
	BuiltinContactsSend  = "contacts_send"
	BuiltinAgentSend     = "agent_send"
	BuiltinSpawn         = "spawn"
	BuiltinACPSpawn      = "acp_spawn"
	BuiltinCoder         = "coder"
)

var builtinToolNameSet = map[string]struct{}{
	BuiltinReadFile:      {},
	BuiltinWriteFile:     {},
	BuiltinBash:          {},
	BuiltinPowerShell:    {},
	BuiltinURLFetch:      {},
	BuiltinWebSearch:     {},
	BuiltinPlanCreate:    {},
	BuiltinTodoUpdate:    {},
	BuiltinImageGenerate: {},
	BuiltinImageEdit:     {},
	BuiltinContactsSend:  {},
	BuiltinAgentSend:     {},
	BuiltinSpawn:         {},
	BuiltinACPSpawn:      {},
	BuiltinCoder:         {},
}

type StaticRegistryConfig struct {
	Common       StaticCommonConfig
	ReadFile     StaticReadFileConfig
	WriteFile    StaticWriteFileConfig
	Bash         StaticBashConfig
	PowerShell   StaticPowerShellConfig
	URLFetch     StaticURLFetchConfig
	WebSearch    StaticWebSearchConfig
	ContactsSend StaticContactsSendConfig
}

type StaticCommonConfig struct {
	UserAgent                   string
	PathRoots                   pathroots.PathRoots
	AuthenticatedHTTPConfigured bool
	Awareness                   bool
}

type StaticReadFileConfig struct {
	MaxBytes  int64
	DenyPaths []string
}

type StaticWriteFileConfig struct {
	Enabled  bool
	MaxBytes int
}

type StaticBashConfig struct {
	Enabled         bool
	Timeout         time.Duration
	MaxOutputBytes  int
	DenyPaths       []string
	PathExtra       []string
	InjectedEnvVars []shellenv.InjectedEnvVar
	Rewrite         builtin.BashRewriteConfig
}

type StaticPowerShellConfig struct {
	Enabled         bool
	Timeout         time.Duration
	MaxOutputBytes  int
	DenyPaths       []string
	InjectedEnvVars []shellenv.InjectedEnvVar
}

type StaticURLFetchConfig struct {
	Enabled          bool
	Timeout          time.Duration
	MaxBytes         int64
	MaxBytesDownload int64
	Auth             *builtin.URLFetchAuth
}

type StaticWebSearchConfig struct {
	Enabled    bool
	Timeout    time.Duration
	MaxResults int
	BaseURL    string
}

type StaticContactsSendConfig struct {
	Enabled           bool
	ContactsDir       string
	TelegramBotToken  string
	TelegramBaseURL   string
	SlackBotToken     string
	SlackBaseURL      string
	LineChannelToken  string
	LineBaseURL       string
	LarkAppID         string
	LarkAppSecret     string
	LarkBaseURL       string
	MixinKeystoreFile string
	FailureCooldown   time.Duration
}

type StaticRegistryConfigReader interface {
	Get(string) any
	GetBool(string) bool
	GetDuration(string) time.Duration
	GetInt(string) int
	GetInt64(string) int64
	GetString(string) string
	GetStringSlice(string) []string
	IsSet(string) bool
	UnmarshalKey(string, any, ...viper.DecoderConfigOption) error
}

// StaticRegistryConfigFromReader decodes and validates the process-independent
// static tool configuration. Entry points may still choose tools, add triggers,
// and mark an awareness registry when registering this value.
func StaticRegistryConfigFromReader(reader StaticRegistryConfigReader) (StaticRegistryConfig, error) {
	if reader == nil {
		return StaticRegistryConfig{}, fmt.Errorf("static registry config reader is nil")
	}

	decodedAuthProfiles := map[string]secrets.AuthProfile{}
	if err := reader.UnmarshalKey("auth_profiles", &decodedAuthProfiles); err != nil {
		return StaticRegistryConfig{}, fmt.Errorf("decode auth_profiles: %w", err)
	}
	authProfiles := make(map[string]secrets.AuthProfile, len(decodedAuthProfiles))
	for rawID, profile := range decodedAuthProfiles {
		profile.ID = strings.TrimSpace(rawID)
		if err := profile.Validate(); err != nil {
			return StaticRegistryConfig{}, err
		}
		if _, exists := authProfiles[profile.ID]; exists {
			return StaticRegistryConfig{}, fmt.Errorf("duplicate auth profile id after normalization: %q", profile.ID)
		}
		authProfiles[profile.ID] = profile
	}

	allowProfiles := make(map[string]bool)
	for _, id := range reader.GetStringSlice("secrets.allow_profiles") {
		if id = strings.TrimSpace(id); id != "" {
			allowProfiles[id] = true
		}
	}
	authenticatedHTTPConfigured := false
	for id := range allowProfiles {
		if _, ok := authProfiles[id]; ok {
			authenticatedHTTPConfigured = true
			break
		}
	}

	paths := runtimepaths.FromReader(reader)
	failureCooldown := 72 * time.Hour
	if reader.IsSet("contacts.proactive.failure_cooldown") {
		if configured := reader.GetDuration("contacts.proactive.failure_cooldown"); configured > 0 {
			failureCooldown = configured
		}
	}

	return StaticRegistryConfig{
		Common: StaticCommonConfig{
			UserAgent:                   strings.TrimSpace(reader.GetString("user_agent")),
			PathRoots:                   pathroots.New("", paths.CacheDir, paths.StateDir),
			AuthenticatedHTTPConfigured: authenticatedHTTPConfigured,
		},
		ReadFile: StaticReadFileConfig{
			MaxBytes:  reader.GetInt64("tools.read_file.max_bytes"),
			DenyPaths: append([]string(nil), reader.GetStringSlice("tools.read_file.deny_paths")...),
		},
		WriteFile: StaticWriteFileConfig{
			Enabled:  reader.GetBool("tools.write_file.enabled"),
			MaxBytes: reader.GetInt("tools.write_file.max_bytes"),
		},
		Bash: StaticBashConfig{
			Enabled:         reader.GetBool("tools.bash.enabled"),
			Timeout:         reader.GetDuration("tools.bash.timeout"),
			MaxOutputBytes:  reader.GetInt("tools.bash.max_output_bytes"),
			DenyPaths:       append([]string(nil), reader.GetStringSlice("tools.bash.deny_paths")...),
			PathExtra:       append([]string(nil), reader.GetStringSlice("tools.bash.path_extra")...),
			InjectedEnvVars: shellenv.InjectedEnvVarsFromConfig(reader.Get("tools.bash.injected_env_vars")),
			Rewrite: builtin.BashRewriteConfig{
				Enabled: reader.GetBool("tools.bash.rewrite.enabled"),
				Binary:  strings.TrimSpace(reader.GetString("tools.bash.rewrite.binary")),
			},
		},
		PowerShell: StaticPowerShellConfig{
			Enabled:         reader.GetBool("tools.powershell.enabled"),
			Timeout:         reader.GetDuration("tools.powershell.timeout"),
			MaxOutputBytes:  reader.GetInt("tools.powershell.max_output_bytes"),
			DenyPaths:       append([]string(nil), reader.GetStringSlice("tools.powershell.deny_paths")...),
			InjectedEnvVars: shellenv.InjectedEnvVarsFromConfig(reader.Get("tools.powershell.injected_env_vars")),
		},
		URLFetch: StaticURLFetchConfig{
			Enabled:          reader.GetBool("tools.url_fetch.enabled"),
			Timeout:          reader.GetDuration("tools.url_fetch.timeout"),
			MaxBytes:         reader.GetInt64("tools.url_fetch.max_bytes"),
			MaxBytesDownload: reader.GetInt64("tools.url_fetch.max_bytes_download"),
			Auth: &builtin.URLFetchAuth{
				AllowProfiles: allowProfiles,
				Profiles:      secrets.NewProfileStore(authProfiles),
			},
		},
		WebSearch: StaticWebSearchConfig{
			Enabled:    reader.GetBool("tools.web_search.enabled"),
			Timeout:    reader.GetDuration("tools.web_search.timeout"),
			MaxResults: reader.GetInt("tools.web_search.max_results"),
			BaseURL:    strings.TrimSpace(reader.GetString("tools.web_search.base_url")),
		},
		ContactsSend: StaticContactsSendConfig{
			Enabled:           reader.GetBool("tools.contacts_send.enabled"),
			ContactsDir:       paths.ContactsDir,
			TelegramBotToken:  strings.TrimSpace(reader.GetString("telegram.bot_token")),
			TelegramBaseURL:   "https://api.telegram.org",
			SlackBotToken:     strings.TrimSpace(reader.GetString("slack.bot_token")),
			SlackBaseURL:      strings.TrimSpace(reader.GetString("slack.base_url")),
			LineChannelToken:  strings.TrimSpace(reader.GetString("line.channel_access_token")),
			LineBaseURL:       strings.TrimSpace(reader.GetString("line.base_url")),
			LarkAppID:         strings.TrimSpace(reader.GetString("lark.app_id")),
			LarkAppSecret:     strings.TrimSpace(reader.GetString("lark.app_secret")),
			LarkBaseURL:       strings.TrimSpace(reader.GetString("lark.base_url")),
			MixinKeystoreFile: pathutil.ResolveConfigRelativePath(reader.GetString("mixin.keystore_file"), reader.GetString("config")),
			FailureCooldown:   failureCooldown,
		},
	}, nil
}

func IsKnownBuiltinToolName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	_, ok := builtinToolNameSet[name]
	return ok
}

func ExplicitBuiltinToolRefs(task string, consumed map[string]bool) map[string]bool {
	names := caprefs.Names(task)
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if consumed[name] {
			continue
		}
		if !IsKnownBuiltinToolName(name) {
			continue
		}
		out[name] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func BuiltinToolTriggers(task string, consumed map[string]bool) map[string]bool {
	return AddImageToolIntentTriggers(ExplicitBuiltinToolRefs(task, consumed), task, false)
}

func AddImageToolIntentTriggers(triggers map[string]bool, task string, activeImage bool) map[string]bool {
	if !ImageToolIntentMatches(task, activeImage) {
		return triggers
	}
	if triggers == nil {
		triggers = make(map[string]bool, 2)
	}
	triggers[BuiltinImageGenerate] = true
	triggers[BuiltinImageEdit] = true
	return triggers
}

func RegisterStaticTools(reg *tools.Registry, cfg StaticRegistryConfig, selected map[string]bool, triggers map[string]bool) {
	if reg == nil {
		return
	}
	isSelected := func(name string) bool {
		if len(selected) == 0 {
			return true
		}
		return selected[name]
	}
	isEnabled := func(name string, enabled bool) bool {
		return enabled || triggers[name]
	}

	if isSelected(BuiltinReadFile) {
		_ = reg.Replace(builtin.NewReadFileToolWithDenyPaths(
			cfg.ReadFile.MaxBytes,
			append([]string(nil), cfg.ReadFile.DenyPaths...),
			cfg.Common.PathRoots,
		))
	}

	if isSelected(BuiltinWriteFile) && isEnabled(BuiltinWriteFile, cfg.WriteFile.Enabled) {
		_ = reg.Replace(builtin.NewWriteFileTool(
			true,
			cfg.WriteFile.MaxBytes,
			cfg.Common.PathRoots,
		))
	}

	if isSelected(BuiltinBash) && isEnabled(BuiltinBash, cfg.Bash.Enabled) {
		bt := builtin.NewBashTool(
			true,
			cfg.Bash.Timeout,
			cfg.Bash.MaxOutputBytes,
			cfg.Common.PathRoots,
		)
		bt.DenyPaths = append([]string(nil), cfg.Bash.DenyPaths...)
		bt.PathExtra = append([]string(nil), cfg.Bash.PathExtra...)
		bt.InjectedEnvVars = shellenv.CloneInjectedEnvVars(cfg.Bash.InjectedEnvVars)
		bt.Rewrite = cfg.Bash.Rewrite
		if cfg.Common.AuthenticatedHTTPConfigured {
			// Safety default: allow bash for local automation, but deny curl when authenticated HTTP is configured.
			bt.DenyTokens = append(bt.DenyTokens, "curl")
		}
		_ = reg.Replace(bt)
	}

	if isSelected(BuiltinPowerShell) && isEnabled(BuiltinPowerShell, cfg.PowerShell.Enabled) {
		pt := builtin.NewPowerShellTool(
			true,
			cfg.PowerShell.Timeout,
			cfg.PowerShell.MaxOutputBytes,
			cfg.Common.PathRoots,
		)
		pt.DenyPaths = append([]string(nil), cfg.PowerShell.DenyPaths...)
		pt.InjectedEnvVars = shellenv.CloneInjectedEnvVars(cfg.PowerShell.InjectedEnvVars)
		if cfg.Common.AuthenticatedHTTPConfigured {
			pt.DenyTokens = append(pt.DenyTokens, "curl")
		}
		_ = reg.Replace(pt)
	}

	if isSelected(BuiltinURLFetch) && isEnabled(BuiltinURLFetch, cfg.URLFetch.Enabled) {
		_ = reg.Replace(builtin.NewURLFetchToolWithAuthLimits(
			true,
			cfg.URLFetch.Timeout,
			cfg.URLFetch.MaxBytes,
			cfg.URLFetch.MaxBytesDownload,
			strings.TrimSpace(cfg.Common.UserAgent),
			strings.TrimSpace(cfg.Common.PathRoots.FileCacheDir),
			cfg.URLFetch.Auth,
		))
	}

	if isSelected(BuiltinWebSearch) && isEnabled(BuiltinWebSearch, cfg.WebSearch.Enabled) {
		_ = reg.Replace(builtin.NewWebSearchTool(
			true,
			cfg.WebSearch.BaseURL,
			cfg.WebSearch.Timeout,
			cfg.WebSearch.MaxResults,
			strings.TrimSpace(cfg.Common.UserAgent),
		))
	}

	contactSendOpts := builtin.ContactsSendToolOptions{
		Enabled:           true,
		ContactsDir:       cfg.ContactsSend.ContactsDir,
		TelegramBotToken:  strings.TrimSpace(cfg.ContactsSend.TelegramBotToken),
		TelegramBaseURL:   strings.TrimSpace(cfg.ContactsSend.TelegramBaseURL),
		SlackBotToken:     strings.TrimSpace(cfg.ContactsSend.SlackBotToken),
		SlackBaseURL:      strings.TrimSpace(cfg.ContactsSend.SlackBaseURL),
		LineChannelToken:  strings.TrimSpace(cfg.ContactsSend.LineChannelToken),
		LineBaseURL:       strings.TrimSpace(cfg.ContactsSend.LineBaseURL),
		LarkAppID:         strings.TrimSpace(cfg.ContactsSend.LarkAppID),
		LarkAppSecret:     strings.TrimSpace(cfg.ContactsSend.LarkAppSecret),
		LarkBaseURL:       strings.TrimSpace(cfg.ContactsSend.LarkBaseURL),
		MixinKeystoreFile: strings.TrimSpace(cfg.ContactsSend.MixinKeystoreFile),
		FailureCooldown:   cfg.ContactsSend.FailureCooldown,
	}
	if isSelected(BuiltinAgentSend) {
		available, err := builtin.AgentSendAvailable(context.Background(), cfg.ContactsSend.ContactsDir)
		if err == nil && available {
			_ = reg.Replace(builtin.NewAgentSendTool(contactSendOpts))
		}
	}

	contactsSendEnabled := cfg.Common.Awareness && cfg.ContactsSend.Enabled
	if isSelected(BuiltinContactsSend) && isEnabled(BuiltinContactsSend, contactsSendEnabled) {
		_ = reg.Replace(builtin.NewContactsSendTool(contactSendOpts))
	}
}
