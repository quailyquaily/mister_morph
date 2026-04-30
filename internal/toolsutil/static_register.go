package toolsutil

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
)

const (
	BuiltinReadFile     = "read_file"
	BuiltinWriteFile    = "write_file"
	BuiltinBash         = "bash"
	BuiltinPowerShell   = "powershell"
	BuiltinURLFetch     = "url_fetch"
	BuiltinWebSearch    = "web_search"
	BuiltinPlanCreate   = "plan_create"
	BuiltinTodoUpdate   = "todo_update"
	BuiltinContactsSend = "contacts_send"
	BuiltinSpawn        = "spawn"
	BuiltinACPSpawn     = "acp_spawn"
)

var builtinToolNameSet = map[string]struct{}{
	BuiltinReadFile:     {},
	BuiltinWriteFile:    {},
	BuiltinBash:         {},
	BuiltinPowerShell:   {},
	BuiltinURLFetch:     {},
	BuiltinWebSearch:    {},
	BuiltinPlanCreate:   {},
	BuiltinTodoUpdate:   {},
	BuiltinContactsSend: {},
	BuiltinSpawn:        {},
	BuiltinACPSpawn:     {},
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
	InjectedEnvVars []string
	Rewrite         builtin.BashRewriteConfig
}

type StaticPowerShellConfig struct {
	Enabled         bool
	Timeout         time.Duration
	MaxOutputBytes  int
	DenyPaths       []string
	InjectedEnvVars []string
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
	Enabled          bool
	ContactsDir      string
	TelegramBotToken string
	TelegramBaseURL  string
	SlackBotToken    string
	SlackBaseURL     string
	LineChannelToken string
	LineBaseURL      string
	LarkAppID        string
	LarkAppSecret    string
	LarkBaseURL      string
	FailureCooldown  time.Duration
}

func IsKnownBuiltinToolName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	_, ok := builtinToolNameSet[name]
	return ok
}

func RegisterStaticTools(reg *tools.Registry, cfg StaticRegistryConfig, selected map[string]bool) {
	if reg == nil {
		return
	}
	isSelected := func(name string) bool {
		if len(selected) == 0 {
			return true
		}
		return selected[name]
	}

	if isSelected(BuiltinReadFile) {
		reg.Register(builtin.NewReadFileToolWithDenyPaths(
			cfg.ReadFile.MaxBytes,
			append([]string(nil), cfg.ReadFile.DenyPaths...),
			cfg.Common.PathRoots,
		))
	}

	if isSelected(BuiltinWriteFile) && cfg.WriteFile.Enabled {
		reg.Register(builtin.NewWriteFileTool(
			true,
			cfg.WriteFile.MaxBytes,
			cfg.Common.PathRoots,
		))
	}

	if isSelected(BuiltinBash) && cfg.Bash.Enabled {
		bt := builtin.NewBashTool(
			true,
			cfg.Bash.Timeout,
			cfg.Bash.MaxOutputBytes,
			cfg.Common.PathRoots,
		)
		bt.DenyPaths = append([]string(nil), cfg.Bash.DenyPaths...)
		bt.InjectedEnvVars = append([]string(nil), cfg.Bash.InjectedEnvVars...)
		bt.Rewrite = cfg.Bash.Rewrite
		if cfg.Common.AuthenticatedHTTPConfigured {
			// Safety default: allow bash for local automation, but deny curl when authenticated HTTP is configured.
			bt.DenyTokens = append(bt.DenyTokens, "curl")
		}
		reg.Register(bt)
	}

	if isSelected(BuiltinPowerShell) && cfg.PowerShell.Enabled {
		pt := builtin.NewPowerShellTool(
			true,
			cfg.PowerShell.Timeout,
			cfg.PowerShell.MaxOutputBytes,
			cfg.Common.PathRoots,
		)
		pt.DenyPaths = append([]string(nil), cfg.PowerShell.DenyPaths...)
		pt.InjectedEnvVars = append([]string(nil), cfg.PowerShell.InjectedEnvVars...)
		if cfg.Common.AuthenticatedHTTPConfigured {
			pt.DenyTokens = append(pt.DenyTokens, "curl")
		}
		reg.Register(pt)
	}

	if isSelected(BuiltinURLFetch) && cfg.URLFetch.Enabled {
		reg.Register(builtin.NewURLFetchToolWithAuthLimits(
			true,
			cfg.URLFetch.Timeout,
			cfg.URLFetch.MaxBytes,
			cfg.URLFetch.MaxBytesDownload,
			strings.TrimSpace(cfg.Common.UserAgent),
			strings.TrimSpace(cfg.Common.PathRoots.FileCacheDir),
			cfg.URLFetch.Auth,
		))
	}

	if isSelected(BuiltinWebSearch) && cfg.WebSearch.Enabled {
		reg.Register(builtin.NewWebSearchTool(
			true,
			cfg.WebSearch.BaseURL,
			cfg.WebSearch.Timeout,
			cfg.WebSearch.MaxResults,
			strings.TrimSpace(cfg.Common.UserAgent),
		))
	}

	if isSelected(BuiltinContactsSend) && cfg.ContactsSend.Enabled {
		reg.Register(builtin.NewContactsSendTool(builtin.ContactsSendToolOptions{
			Enabled:          true,
			ContactsDir:      cfg.ContactsSend.ContactsDir,
			TelegramBotToken: strings.TrimSpace(cfg.ContactsSend.TelegramBotToken),
			TelegramBaseURL:  strings.TrimSpace(cfg.ContactsSend.TelegramBaseURL),
			SlackBotToken:    strings.TrimSpace(cfg.ContactsSend.SlackBotToken),
			SlackBaseURL:     strings.TrimSpace(cfg.ContactsSend.SlackBaseURL),
			LineChannelToken: strings.TrimSpace(cfg.ContactsSend.LineChannelToken),
			LineBaseURL:      strings.TrimSpace(cfg.ContactsSend.LineBaseURL),
			LarkAppID:        strings.TrimSpace(cfg.ContactsSend.LarkAppID),
			LarkAppSecret:    strings.TrimSpace(cfg.ContactsSend.LarkAppSecret),
			LarkBaseURL:      strings.TrimSpace(cfg.ContactsSend.LarkBaseURL),
			FailureCooldown:  cfg.ContactsSend.FailureCooldown,
		}))
	}
}
