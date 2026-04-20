package integration

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/secrets"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
)

func (rt *Runtime) buildRegistry(cfg registrySnapshot, logger *slog.Logger) *tools.Registry {
	r := tools.NewRegistry()
	if rt == nil {
		return r
	}
	if logger == nil {
		logger = slog.Default()
	}

	selectedBuiltinTools := make(map[string]bool, len(rt.builtinToolNames))
	for _, name := range rt.builtinToolNames {
		selectedBuiltinTools[name] = true
		if !toolsutil.IsKnownBuiltinToolName(name) {
			logger.Warn("unknown_builtin_tool_name", "name", name)
		}
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
			logger.Warn("auth_profile_invalid", "profile", p.ID, "err", err)
			delete(authProfiles, p.ID)
		}
	}

	logger.Info("auth_profiles_configured",
		"allow_profiles", keysSorted(allowProfiles),
		"auth_profiles", len(authProfiles),
	)

	profileStore := secrets.NewProfileStore(authProfiles)
	authenticatedHTTPConfigured := hasAllowedAuthProfiles(allowProfiles, authProfiles)

	toolsutil.RegisterStaticTools(r, toolsutil.StaticRegistryConfig{
		Common: toolsutil.StaticCommonConfig{
			UserAgent:                   userAgent,
			PathRoots:                   cfg.PathRoots,
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
			TelegramBotToken: strings.TrimSpace(cfg.TelegramBotToken),
			TelegramBaseURL:  strings.TrimSpace(cfg.TelegramBaseURL),
			SlackBotToken:    strings.TrimSpace(cfg.SlackBotToken),
			SlackBaseURL:     strings.TrimSpace(cfg.SlackBaseURL),
			LineChannelToken: strings.TrimSpace(cfg.LineChannelAccessToken),
			LineBaseURL:      strings.TrimSpace(cfg.LineBaseURL),
			FailureCooldown:  cfg.ContactsFailureCooldown,
		},
	}, selectedBuiltinTools)

	return r
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

func hasAllowedAuthProfiles(allowProfiles map[string]bool, authProfiles map[string]secrets.AuthProfile) bool {
	for id := range allowProfiles {
		if _, ok := authProfiles[id]; ok {
			return true
		}
	}
	return false
}
