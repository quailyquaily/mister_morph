package consolecmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/integration"
	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	"github.com/quailyquaily/mistermorph/internal/configbootstrap"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/secref"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	consoleSettingsKey           = "console"
	consoleGroupTriggerStrict    = "strict"
	consoleGroupTriggerSmart     = "smart"
	consoleGroupTriggerTalkative = "talkative"
)

type consoleTelegramSettingsPayload struct {
	BotToken         string   `json:"bot_token"`
	AllowedChatIDs   []string `json:"allowed_chat_ids"`
	GroupTriggerMode string   `json:"group_trigger_mode"`
}

type consoleSlackSettingsPayload struct {
	BotToken          string   `json:"bot_token"`
	AppToken          string   `json:"app_token"`
	AllowedTeamIDs    []string `json:"allowed_team_ids"`
	AllowedChannelIDs []string `json:"allowed_channel_ids"`
	GroupTriggerMode  string   `json:"group_trigger_mode"`
}

type consoleLineSettingsPayload struct {
	ChannelAccessToken string   `json:"channel_access_token"`
	ChannelSecret      string   `json:"channel_secret"`
	AllowedGroupIDs    []string `json:"allowed_group_ids"`
	GroupTriggerMode   string   `json:"group_trigger_mode"`
}

type consoleLarkSettingsPayload struct {
	AppID            string   `json:"app_id"`
	AppSecret        string   `json:"app_secret"`
	AllowedChatIDs   []string `json:"allowed_chat_ids"`
	GroupTriggerMode string   `json:"group_trigger_mode"`
}

type consoleGuardURLFetchSettingsPayload struct {
	AllowedURLPrefixes []string `json:"allowed_url_prefixes"`
	DenyPrivateIPs     bool     `json:"deny_private_ips"`
	FollowRedirects    bool     `json:"follow_redirects"`
	AllowProxy         bool     `json:"allow_proxy"`
}

type consoleGuardNetworkSettingsPayload struct {
	URLFetch consoleGuardURLFetchSettingsPayload `json:"url_fetch"`
}

type consoleGuardRedactionSettingsPayload struct {
	Enabled bool `json:"enabled"`
}

type consoleGuardApprovalsSettingsPayload struct {
	Enabled bool `json:"enabled"`
}

type consoleGuardSettingsPayload struct {
	Enabled   bool                                 `json:"enabled"`
	Network   consoleGuardNetworkSettingsPayload   `json:"network"`
	Redaction consoleGuardRedactionSettingsPayload `json:"redaction"`
	Approvals consoleGuardApprovalsSettingsPayload `json:"approvals"`
}

type consoleSettingsPayload struct {
	ManagedRuntimes []string                       `json:"managed_runtimes"`
	Telegram        consoleTelegramSettingsPayload `json:"telegram"`
	Slack           consoleSlackSettingsPayload    `json:"slack"`
	Line            consoleLineSettingsPayload     `json:"line"`
	Lark            consoleLarkSettingsPayload     `json:"lark"`
	Guard           consoleGuardSettingsPayload    `json:"guard"`
}

type consoleTelegramSettingsUpdatePayload struct {
	BotToken         *string   `json:"bot_token,omitempty"`
	AllowedChatIDs   *[]string `json:"allowed_chat_ids,omitempty"`
	GroupTriggerMode *string   `json:"group_trigger_mode,omitempty"`
}

type consoleSlackSettingsUpdatePayload struct {
	BotToken          *string   `json:"bot_token,omitempty"`
	AppToken          *string   `json:"app_token,omitempty"`
	AllowedTeamIDs    *[]string `json:"allowed_team_ids,omitempty"`
	AllowedChannelIDs *[]string `json:"allowed_channel_ids,omitempty"`
	GroupTriggerMode  *string   `json:"group_trigger_mode,omitempty"`
}

type consoleLineSettingsUpdatePayload struct {
	ChannelAccessToken *string   `json:"channel_access_token,omitempty"`
	ChannelSecret      *string   `json:"channel_secret,omitempty"`
	AllowedGroupIDs    *[]string `json:"allowed_group_ids,omitempty"`
	GroupTriggerMode   *string   `json:"group_trigger_mode,omitempty"`
}

type consoleLarkSettingsUpdatePayload struct {
	AppID            *string   `json:"app_id,omitempty"`
	AppSecret        *string   `json:"app_secret,omitempty"`
	AllowedChatIDs   *[]string `json:"allowed_chat_ids,omitempty"`
	GroupTriggerMode *string   `json:"group_trigger_mode,omitempty"`
}

type consoleGuardURLFetchSettingsUpdatePayload struct {
	AllowedURLPrefixes *[]string `json:"allowed_url_prefixes,omitempty"`
	DenyPrivateIPs     *bool     `json:"deny_private_ips,omitempty"`
	FollowRedirects    *bool     `json:"follow_redirects,omitempty"`
	AllowProxy         *bool     `json:"allow_proxy,omitempty"`
}

type consoleGuardNetworkSettingsUpdatePayload struct {
	URLFetch *consoleGuardURLFetchSettingsUpdatePayload `json:"url_fetch,omitempty"`
}

type consoleGuardRedactionSettingsUpdatePayload struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type consoleGuardApprovalsSettingsUpdatePayload struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type consoleGuardSettingsUpdatePayload struct {
	Enabled   *bool                                       `json:"enabled,omitempty"`
	Network   *consoleGuardNetworkSettingsUpdatePayload   `json:"network,omitempty"`
	Redaction *consoleGuardRedactionSettingsUpdatePayload `json:"redaction,omitempty"`
	Approvals *consoleGuardApprovalsSettingsUpdatePayload `json:"approvals,omitempty"`
}

type consoleSettingsUpdatePayload struct {
	ManagedRuntimes *[]string                             `json:"managed_runtimes,omitempty"`
	Telegram        *consoleTelegramSettingsUpdatePayload `json:"telegram,omitempty"`
	Slack           *consoleSlackSettingsUpdatePayload    `json:"slack,omitempty"`
	Line            *consoleLineSettingsUpdatePayload     `json:"line,omitempty"`
	Lark            *consoleLarkSettingsUpdatePayload     `json:"lark,omitempty"`
	Guard           *consoleGuardSettingsUpdatePayload    `json:"guard,omitempty"`
}

type consoleSettingsEnvManagedPayload struct {
	Telegram map[string]agentsettings.EnvManagedField `json:"telegram,omitempty"`
	Slack    map[string]agentsettings.EnvManagedField `json:"slack,omitempty"`
	Line     map[string]agentsettings.EnvManagedField `json:"line,omitempty"`
	Lark     map[string]agentsettings.EnvManagedField `json:"lark,omitempty"`
}

func (s *server) handleConsoleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleConsoleSettingsGet(w, r)
	case http.MethodPut:
		s.handleConsoleSettingsPut(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleConsoleSettingsGet(w http.ResponseWriter, _ *http.Request) {
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, err := readConsoleSettings(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	doc := configbootstrap.NewEmptyDocument()
	if raw, readErr := os.ReadFile(configPath); readErr == nil && len(bytes.TrimSpace(raw)) > 0 {
		doc, err = configbootstrap.LoadDocumentBytes(raw)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	settings, envManaged := buildConsoleSettingsResponseView(settings, doc)
	writeJSON(w, http.StatusOK, map[string]any{
		"managed_runtimes": settings.ManagedRuntimes,
		"telegram":         settings.Telegram,
		"slack":            settings.Slack,
		"line":             settings.Line,
		"lark":             settings.Lark,
		"guard":            settings.Guard,
		"env_managed":      envManaged,
		"config_path":      configPath,
	})
}

func (s *server) handleConsoleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var req consoleSettingsUpdatePayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	configPath, err := resolveConsoleConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	current, err := readConsoleSettings(configPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, err := normalizeConsoleSettingsUpdatePayload(current, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	serialized, err := writeConsoleSettings(configPath, next)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := fsstore.WriteTextAtomic(configPath, string(serialized), fsstore.FileOptions{DirPerm: 0o755, FilePerm: 0o600}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	doc, docErr := configbootstrap.LoadDocumentBytes(serialized)
	if docErr != nil {
		writeError(w, http.StatusInternalServerError, docErr.Error())
		return
	}
	next, envManaged := buildConsoleSettingsResponseView(next, doc)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"managed_runtimes": next.ManagedRuntimes,
		"telegram":         next.Telegram,
		"slack":            next.Slack,
		"line":             next.Line,
		"lark":             next.Lark,
		"guard":            next.Guard,
		"env_managed":      envManaged,
		"config_path":      configPath,
	})
}

func readConsoleSettings(configPath string) (consoleSettingsPayload, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConsoleSettingsPayload(), nil
		}
		return consoleSettingsPayload{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return defaultConsoleSettingsPayload(), nil
	}
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	tmp.SetConfigType("yaml")
	if err := tmp.ReadConfig(bytes.NewReader(data)); err != nil {
		return consoleSettingsPayload{}, fmt.Errorf("invalid config yaml: %w", err)
	}
	return readConsoleSettingsFromReader(tmp), nil
}

func defaultConsoleSettingsPayload() consoleSettingsPayload {
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	return readConsoleSettingsFromReader(tmp)
}

func readExpandedConsoleSettingsConfig(configPath string) (*viper.Viper, error) {
	tmp := viper.New()
	integration.ApplyViperDefaults(tmp)
	if err := configutil.ReadExpandedConfig(tmp, configPath, nil); err != nil {
		return nil, err
	}
	return tmp, nil
}

func writeConsoleSettings(configPath string, values consoleSettingsPayload) ([]byte, error) {
	doc, err := loadYAMLDocument(configPath)
	if err != nil {
		return nil, err
	}
	root, err := configbootstrap.DocumentMapping(doc)
	if err != nil {
		return nil, err
	}
	consoleNode := configbootstrap.EnsureMappingValue(root, consoleSettingsKey)
	setMappingOrderedStringList(consoleNode, "managed_runtimes", values.ManagedRuntimes)

	telegramNode := configbootstrap.EnsureMappingValue(root, "telegram")
	configbootstrap.SetOrDeleteMappingScalar(telegramNode, "bot_token", strings.TrimSpace(values.Telegram.BotToken))
	setMappingOrderedStringList(telegramNode, "allowed_chat_ids", normalizeConsoleStringList(values.Telegram.AllowedChatIDs))
	configbootstrap.SetOrDeleteMappingScalar(telegramNode, "group_trigger_mode", strings.TrimSpace(values.Telegram.GroupTriggerMode))

	slackNode := configbootstrap.EnsureMappingValue(root, "slack")
	configbootstrap.SetOrDeleteMappingScalar(slackNode, "bot_token", strings.TrimSpace(values.Slack.BotToken))
	configbootstrap.SetOrDeleteMappingScalar(slackNode, "app_token", strings.TrimSpace(values.Slack.AppToken))
	setMappingOrderedStringList(slackNode, "allowed_team_ids", normalizeConsoleStringList(values.Slack.AllowedTeamIDs))
	setMappingOrderedStringList(slackNode, "allowed_channel_ids", normalizeConsoleStringList(values.Slack.AllowedChannelIDs))
	configbootstrap.SetOrDeleteMappingScalar(slackNode, "group_trigger_mode", strings.TrimSpace(values.Slack.GroupTriggerMode))

	lineNode := configbootstrap.EnsureMappingValue(root, "line")
	configbootstrap.SetOrDeleteMappingScalar(lineNode, "channel_access_token", strings.TrimSpace(values.Line.ChannelAccessToken))
	configbootstrap.SetOrDeleteMappingScalar(lineNode, "channel_secret", strings.TrimSpace(values.Line.ChannelSecret))
	setMappingOrderedStringList(lineNode, "allowed_group_ids", normalizeConsoleStringList(values.Line.AllowedGroupIDs))
	configbootstrap.SetOrDeleteMappingScalar(lineNode, "group_trigger_mode", strings.TrimSpace(values.Line.GroupTriggerMode))

	larkNode := configbootstrap.EnsureMappingValue(root, "lark")
	configbootstrap.SetOrDeleteMappingScalar(larkNode, "app_id", strings.TrimSpace(values.Lark.AppID))
	configbootstrap.SetOrDeleteMappingScalar(larkNode, "app_secret", strings.TrimSpace(values.Lark.AppSecret))
	setMappingOrderedStringList(larkNode, "allowed_chat_ids", normalizeConsoleStringList(values.Lark.AllowedChatIDs))
	configbootstrap.SetOrDeleteMappingScalar(larkNode, "group_trigger_mode", strings.TrimSpace(values.Lark.GroupTriggerMode))

	guardNode := configbootstrap.EnsureMappingValue(root, "guard")
	configbootstrap.SetMappingBoolValue(guardNode, "enabled", values.Guard.Enabled)
	networkNode := configbootstrap.EnsureMappingValue(guardNode, "network")
	urlFetchNode := configbootstrap.EnsureMappingValue(networkNode, "url_fetch")
	setMappingOrderedStringList(urlFetchNode, "allowed_url_prefixes", normalizeConsoleStringList(values.Guard.Network.URLFetch.AllowedURLPrefixes))
	configbootstrap.SetMappingBoolValue(urlFetchNode, "deny_private_ips", values.Guard.Network.URLFetch.DenyPrivateIPs)
	configbootstrap.SetMappingBoolValue(urlFetchNode, "follow_redirects", values.Guard.Network.URLFetch.FollowRedirects)
	configbootstrap.SetMappingBoolValue(urlFetchNode, "allow_proxy", values.Guard.Network.URLFetch.AllowProxy)
	redactionNode := configbootstrap.EnsureMappingValue(guardNode, "redaction")
	configbootstrap.SetMappingBoolValue(redactionNode, "enabled", values.Guard.Redaction.Enabled)
	approvalsNode := configbootstrap.EnsureMappingValue(guardNode, "approvals")
	configbootstrap.SetMappingBoolValue(approvalsNode, "enabled", values.Guard.Approvals.Enabled)

	return configbootstrap.MarshalDocument(doc)
}

func readConsoleSettingsFromReader(r interface {
	GetStringSlice(string) []string
	GetString(string) string
	GetBool(string) bool
}) consoleSettingsPayload {
	if r == nil {
		return consoleSettingsPayload{}
	}
	managedKinds, _ := normalizeManagedRuntimeKinds(r.GetStringSlice("console.managed_runtimes"))
	return consoleSettingsPayload{
		ManagedRuntimes: managedKinds,
		Telegram: consoleTelegramSettingsPayload{
			BotToken:         strings.TrimSpace(r.GetString("telegram.bot_token")),
			AllowedChatIDs:   normalizeConsoleStringList(r.GetStringSlice("telegram.allowed_chat_ids")),
			GroupTriggerMode: normalizeConsoleGroupTriggerMode(strings.TrimSpace(r.GetString("telegram.group_trigger_mode"))),
		},
		Slack: consoleSlackSettingsPayload{
			BotToken:          strings.TrimSpace(r.GetString("slack.bot_token")),
			AppToken:          strings.TrimSpace(r.GetString("slack.app_token")),
			AllowedTeamIDs:    normalizeConsoleStringList(r.GetStringSlice("slack.allowed_team_ids")),
			AllowedChannelIDs: normalizeConsoleStringList(r.GetStringSlice("slack.allowed_channel_ids")),
			GroupTriggerMode:  normalizeConsoleGroupTriggerMode(strings.TrimSpace(r.GetString("slack.group_trigger_mode"))),
		},
		Line: consoleLineSettingsPayload{
			ChannelAccessToken: strings.TrimSpace(r.GetString("line.channel_access_token")),
			ChannelSecret:      strings.TrimSpace(r.GetString("line.channel_secret")),
			AllowedGroupIDs:    normalizeConsoleStringList(r.GetStringSlice("line.allowed_group_ids")),
			GroupTriggerMode:   normalizeConsoleGroupTriggerMode(strings.TrimSpace(r.GetString("line.group_trigger_mode"))),
		},
		Lark: consoleLarkSettingsPayload{
			AppID:            strings.TrimSpace(r.GetString("lark.app_id")),
			AppSecret:        strings.TrimSpace(r.GetString("lark.app_secret")),
			AllowedChatIDs:   normalizeConsoleStringList(r.GetStringSlice("lark.allowed_chat_ids")),
			GroupTriggerMode: normalizeConsoleGroupTriggerMode(strings.TrimSpace(r.GetString("lark.group_trigger_mode"))),
		},
		Guard: consoleGuardSettingsPayload{
			Enabled: r.GetBool("guard.enabled"),
			Network: consoleGuardNetworkSettingsPayload{
				URLFetch: consoleGuardURLFetchSettingsPayload{
					AllowedURLPrefixes: normalizeConsoleStringList(r.GetStringSlice("guard.network.url_fetch.allowed_url_prefixes")),
					DenyPrivateIPs:     r.GetBool("guard.network.url_fetch.deny_private_ips"),
					FollowRedirects:    r.GetBool("guard.network.url_fetch.follow_redirects"),
					AllowProxy:         r.GetBool("guard.network.url_fetch.allow_proxy"),
				},
			},
			Redaction: consoleGuardRedactionSettingsPayload{
				Enabled: r.GetBool("guard.redaction.enabled"),
			},
			Approvals: consoleGuardApprovalsSettingsPayload{
				Enabled: r.GetBool("guard.approvals.enabled"),
			},
		},
	}
}

func normalizeConsoleSettingsPayload(in consoleSettingsPayload) (consoleSettingsPayload, error) {
	managedKinds, err := normalizeManagedRuntimeKinds(in.ManagedRuntimes)
	if err != nil {
		return consoleSettingsPayload{}, err
	}
	telegramAllowed := normalizeConsoleStringList(in.Telegram.AllowedChatIDs)
	if _, err := channelopts.ParseTelegramAllowedChatIDs(telegramAllowed); err != nil {
		return consoleSettingsPayload{}, err
	}
	return consoleSettingsPayload{
		ManagedRuntimes: managedKinds,
		Telegram: consoleTelegramSettingsPayload{
			BotToken:         strings.TrimSpace(in.Telegram.BotToken),
			AllowedChatIDs:   telegramAllowed,
			GroupTriggerMode: normalizeConsoleGroupTriggerMode(strings.TrimSpace(in.Telegram.GroupTriggerMode)),
		},
		Slack: consoleSlackSettingsPayload{
			BotToken:          strings.TrimSpace(in.Slack.BotToken),
			AppToken:          strings.TrimSpace(in.Slack.AppToken),
			AllowedTeamIDs:    normalizeConsoleStringList(in.Slack.AllowedTeamIDs),
			AllowedChannelIDs: normalizeConsoleStringList(in.Slack.AllowedChannelIDs),
			GroupTriggerMode:  normalizeConsoleGroupTriggerMode(strings.TrimSpace(in.Slack.GroupTriggerMode)),
		},
		Line: consoleLineSettingsPayload{
			ChannelAccessToken: strings.TrimSpace(in.Line.ChannelAccessToken),
			ChannelSecret:      strings.TrimSpace(in.Line.ChannelSecret),
			AllowedGroupIDs:    normalizeConsoleStringList(in.Line.AllowedGroupIDs),
			GroupTriggerMode:   normalizeConsoleGroupTriggerMode(strings.TrimSpace(in.Line.GroupTriggerMode)),
		},
		Lark: consoleLarkSettingsPayload{
			AppID:            strings.TrimSpace(in.Lark.AppID),
			AppSecret:        strings.TrimSpace(in.Lark.AppSecret),
			AllowedChatIDs:   normalizeConsoleStringList(in.Lark.AllowedChatIDs),
			GroupTriggerMode: normalizeConsoleGroupTriggerMode(strings.TrimSpace(in.Lark.GroupTriggerMode)),
		},
		Guard: consoleGuardSettingsPayload{
			Enabled: in.Guard.Enabled,
			Network: consoleGuardNetworkSettingsPayload{
				URLFetch: consoleGuardURLFetchSettingsPayload{
					AllowedURLPrefixes: normalizeConsoleStringList(in.Guard.Network.URLFetch.AllowedURLPrefixes),
					DenyPrivateIPs:     in.Guard.Network.URLFetch.DenyPrivateIPs,
					FollowRedirects:    in.Guard.Network.URLFetch.FollowRedirects,
					AllowProxy:         in.Guard.Network.URLFetch.AllowProxy,
				},
			},
			Redaction: consoleGuardRedactionSettingsPayload{
				Enabled: in.Guard.Redaction.Enabled,
			},
			Approvals: consoleGuardApprovalsSettingsPayload{
				Enabled: in.Guard.Approvals.Enabled,
			},
		},
	}, nil
}

func normalizeConsoleSettingsUpdatePayload(
	current consoleSettingsPayload,
	in consoleSettingsUpdatePayload,
) (consoleSettingsPayload, error) {
	next := current
	if in.ManagedRuntimes != nil {
		managedKinds, err := normalizeManagedRuntimeKinds(*in.ManagedRuntimes)
		if err != nil {
			return consoleSettingsPayload{}, err
		}
		next.ManagedRuntimes = managedKinds
	}
	if in.Telegram != nil {
		if in.Telegram.BotToken != nil {
			next.Telegram.BotToken = strings.TrimSpace(*in.Telegram.BotToken)
		}
		if in.Telegram.AllowedChatIDs != nil {
			next.Telegram.AllowedChatIDs = normalizeConsoleStringList(*in.Telegram.AllowedChatIDs)
		}
		if in.Telegram.GroupTriggerMode != nil {
			next.Telegram.GroupTriggerMode = normalizeConsoleGroupTriggerMode(*in.Telegram.GroupTriggerMode)
		}
	}
	if in.Slack != nil {
		if in.Slack.BotToken != nil {
			next.Slack.BotToken = strings.TrimSpace(*in.Slack.BotToken)
		}
		if in.Slack.AppToken != nil {
			next.Slack.AppToken = strings.TrimSpace(*in.Slack.AppToken)
		}
		if in.Slack.AllowedTeamIDs != nil {
			next.Slack.AllowedTeamIDs = normalizeConsoleStringList(*in.Slack.AllowedTeamIDs)
		}
		if in.Slack.AllowedChannelIDs != nil {
			next.Slack.AllowedChannelIDs = normalizeConsoleStringList(*in.Slack.AllowedChannelIDs)
		}
		if in.Slack.GroupTriggerMode != nil {
			next.Slack.GroupTriggerMode = normalizeConsoleGroupTriggerMode(*in.Slack.GroupTriggerMode)
		}
	}
	if in.Line != nil {
		if in.Line.ChannelAccessToken != nil {
			next.Line.ChannelAccessToken = strings.TrimSpace(*in.Line.ChannelAccessToken)
		}
		if in.Line.ChannelSecret != nil {
			next.Line.ChannelSecret = strings.TrimSpace(*in.Line.ChannelSecret)
		}
		if in.Line.AllowedGroupIDs != nil {
			next.Line.AllowedGroupIDs = normalizeConsoleStringList(*in.Line.AllowedGroupIDs)
		}
		if in.Line.GroupTriggerMode != nil {
			next.Line.GroupTriggerMode = normalizeConsoleGroupTriggerMode(*in.Line.GroupTriggerMode)
		}
	}
	if in.Lark != nil {
		if in.Lark.AppID != nil {
			next.Lark.AppID = strings.TrimSpace(*in.Lark.AppID)
		}
		if in.Lark.AppSecret != nil {
			next.Lark.AppSecret = strings.TrimSpace(*in.Lark.AppSecret)
		}
		if in.Lark.AllowedChatIDs != nil {
			next.Lark.AllowedChatIDs = normalizeConsoleStringList(*in.Lark.AllowedChatIDs)
		}
		if in.Lark.GroupTriggerMode != nil {
			next.Lark.GroupTriggerMode = normalizeConsoleGroupTriggerMode(*in.Lark.GroupTriggerMode)
		}
	}
	if in.Guard != nil {
		if in.Guard.Enabled != nil {
			next.Guard.Enabled = *in.Guard.Enabled
		}
		if in.Guard.Network != nil && in.Guard.Network.URLFetch != nil {
			if in.Guard.Network.URLFetch.AllowedURLPrefixes != nil {
				next.Guard.Network.URLFetch.AllowedURLPrefixes = normalizeConsoleStringList(*in.Guard.Network.URLFetch.AllowedURLPrefixes)
			}
			if in.Guard.Network.URLFetch.DenyPrivateIPs != nil {
				next.Guard.Network.URLFetch.DenyPrivateIPs = *in.Guard.Network.URLFetch.DenyPrivateIPs
			}
			if in.Guard.Network.URLFetch.FollowRedirects != nil {
				next.Guard.Network.URLFetch.FollowRedirects = *in.Guard.Network.URLFetch.FollowRedirects
			}
			if in.Guard.Network.URLFetch.AllowProxy != nil {
				next.Guard.Network.URLFetch.AllowProxy = *in.Guard.Network.URLFetch.AllowProxy
			}
		}
		if in.Guard.Redaction != nil && in.Guard.Redaction.Enabled != nil {
			next.Guard.Redaction.Enabled = *in.Guard.Redaction.Enabled
		}
		if in.Guard.Approvals != nil && in.Guard.Approvals.Enabled != nil {
			next.Guard.Approvals.Enabled = *in.Guard.Approvals.Enabled
		}
	}
	return normalizeConsoleSettingsPayload(next)
}

func normalizeConsoleStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeConsoleGroupTriggerMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case consoleGroupTriggerStrict:
		return consoleGroupTriggerStrict
	case consoleGroupTriggerTalkative:
		return consoleGroupTriggerTalkative
	default:
		return consoleGroupTriggerSmart
	}
}

func buildConsoleSettingsResponseView(
	settings consoleSettingsPayload,
	doc *yaml.Node,
) (consoleSettingsPayload, consoleSettingsEnvManagedPayload) {
	envManaged := currentConsoleSettingsEnvManaged()
	root, _ := configbootstrap.DocumentMapping(doc)
	settings.Telegram, envManaged.Telegram = buildConsoleTelegramSettingsResponseView(
		settings.Telegram,
		configbootstrap.FindMappingValue(root, "telegram"),
		envManaged.Telegram,
	)
	settings.Slack, envManaged.Slack = buildConsoleSlackSettingsResponseView(
		settings.Slack,
		configbootstrap.FindMappingValue(root, "slack"),
		envManaged.Slack,
	)
	settings.Line, envManaged.Line = buildConsoleLineSettingsResponseView(
		settings.Line,
		configbootstrap.FindMappingValue(root, "line"),
		envManaged.Line,
	)
	settings.Lark, envManaged.Lark = buildConsoleLarkSettingsResponseView(
		settings.Lark,
		configbootstrap.FindMappingValue(root, "lark"),
		envManaged.Lark,
	)
	if len(envManaged.Telegram) == 0 {
		envManaged.Telegram = nil
	}
	if len(envManaged.Slack) == 0 {
		envManaged.Slack = nil
	}
	if len(envManaged.Line) == 0 {
		envManaged.Line = nil
	}
	if len(envManaged.Lark) == 0 {
		envManaged.Lark = nil
	}
	return settings, envManaged
}

func buildConsoleTelegramSettingsResponseView(
	settings consoleTelegramSettingsPayload,
	node *yaml.Node,
	envManaged map[string]agentsettings.EnvManagedField,
) (consoleTelegramSettingsPayload, map[string]agentsettings.EnvManagedField) {
	envManaged = applyConsoleSettingsYAMLEnvManaged(node, envManaged, "bot_token")
	if _, ok := envManaged["bot_token"]; ok && consoleSettingsShouldHideSensitiveField(node, "bot_token") {
		settings.BotToken = ""
	}
	if len(envManaged) == 0 {
		return settings, nil
	}
	return settings, envManaged
}

func buildConsoleSlackSettingsResponseView(
	settings consoleSlackSettingsPayload,
	node *yaml.Node,
	envManaged map[string]agentsettings.EnvManagedField,
) (consoleSlackSettingsPayload, map[string]agentsettings.EnvManagedField) {
	envManaged = applyConsoleSettingsYAMLEnvManaged(node, envManaged, "bot_token", "app_token")
	if _, ok := envManaged["bot_token"]; ok && consoleSettingsShouldHideSensitiveField(node, "bot_token") {
		settings.BotToken = ""
	}
	if _, ok := envManaged["app_token"]; ok && consoleSettingsShouldHideSensitiveField(node, "app_token") {
		settings.AppToken = ""
	}
	if len(envManaged) == 0 {
		return settings, nil
	}
	return settings, envManaged
}

func buildConsoleLineSettingsResponseView(
	settings consoleLineSettingsPayload,
	node *yaml.Node,
	envManaged map[string]agentsettings.EnvManagedField,
) (consoleLineSettingsPayload, map[string]agentsettings.EnvManagedField) {
	envManaged = applyConsoleSettingsYAMLEnvManaged(node, envManaged, "channel_access_token", "channel_secret")
	if _, ok := envManaged["channel_access_token"]; ok && consoleSettingsShouldHideSensitiveField(node, "channel_access_token") {
		settings.ChannelAccessToken = ""
	}
	if _, ok := envManaged["channel_secret"]; ok && consoleSettingsShouldHideSensitiveField(node, "channel_secret") {
		settings.ChannelSecret = ""
	}
	if len(envManaged) == 0 {
		return settings, nil
	}
	return settings, envManaged
}

func buildConsoleLarkSettingsResponseView(
	settings consoleLarkSettingsPayload,
	node *yaml.Node,
	envManaged map[string]agentsettings.EnvManagedField,
) (consoleLarkSettingsPayload, map[string]agentsettings.EnvManagedField) {
	envManaged = applyConsoleSettingsYAMLEnvManaged(node, envManaged, "app_id", "app_secret")
	if field, ok := envManaged["app_id"]; ok && strings.TrimSpace(field.Value) != "" {
		settings.AppID = strings.TrimSpace(field.Value)
	}
	if _, ok := envManaged["app_secret"]; ok && consoleSettingsShouldHideSensitiveField(node, "app_secret") {
		settings.AppSecret = ""
	}
	if len(envManaged) == 0 {
		return settings, nil
	}
	return settings, envManaged
}

func applyConsoleSettingsYAMLEnvManaged(
	node *yaml.Node,
	envManaged map[string]agentsettings.EnvManagedField,
	fields ...string,
) map[string]agentsettings.EnvManagedField {
	for _, field := range fields {
		entry, ok := consoleSettingsYAMLManagedField(node, field)
		current, hasCurrent := envManaged[field]
		if hasCurrent {
			if ok && strings.TrimSpace(current.RawValue) == "" {
				current.RawValue = entry.RawValue
			}
			if strings.TrimSpace(current.EnvName) == "" && strings.TrimSpace(entry.EnvName) != "" {
				current.EnvName = entry.EnvName
			}
			if strings.TrimSpace(current.Value) == "" && strings.TrimSpace(entry.Value) != "" {
				current.Value = entry.Value
			}
			envManaged[field] = current
			continue
		}
		if !ok {
			continue
		}
		if envManaged == nil {
			envManaged = map[string]agentsettings.EnvManagedField{}
		}
		envManaged[field] = entry
	}
	return envManaged
}

func consoleSettingsYAMLManagedField(node *yaml.Node, field string) (agentsettings.EnvManagedField, bool) {
	entryNode := configbootstrap.FindMappingValue(node, field)
	if entryNode == nil || entryNode.Kind != yaml.ScalarNode {
		return agentsettings.EnvManagedField{}, false
	}
	value := strings.TrimSpace(entryNode.Value)
	ref, ok := secref.ParseSingleRef(value)
	if !ok {
		return agentsettings.EnvManagedField{}, false
	}
	out := agentsettings.EnvManagedField{
		RawValue: value,
	}
	if ref.Kind == secref.RefKindAWSSecretsManager {
		out.Source = string(secref.RefKindAWSSecretsManager)
		return out, true
	}
	if ref.Kind != secref.RefKindEnv || strings.TrimSpace(ref.EnvName) == "" {
		return agentsettings.EnvManagedField{}, false
	}
	out.EnvName = ref.EnvName
	switch strings.TrimSpace(field) {
	case "bot_token", "app_token", "channel_access_token", "channel_secret", "app_secret":
	default:
		if resolved, ok := os.LookupEnv(ref.EnvName); ok {
			out.Value = strings.TrimSpace(resolved)
		}
	}
	return out, true
}

func consoleSettingsShouldHideSensitiveField(node *yaml.Node, field string) bool {
	entryNode := configbootstrap.FindMappingValue(node, field)
	if entryNode == nil || entryNode.Kind != yaml.ScalarNode {
		return true
	}
	value := strings.TrimSpace(entryNode.Value)
	ref, ok := secref.ParseSingleRef(value)
	return ok && (ref.Kind == secref.RefKindEnv || ref.Kind == secref.RefKindAWSSecretsManager)
}

func currentConsoleSettingsEnvManaged() consoleSettingsEnvManagedPayload {
	var out consoleSettingsEnvManagedPayload
	if field, ok := agentsettings.ManagedEnvField(true, "MISTER_MORPH_TELEGRAM_BOT_TOKEN"); ok {
		out.Telegram = map[string]agentsettings.EnvManagedField{"bot_token": field}
	}
	if field, ok := agentsettings.ManagedEnvField(true, "MISTER_MORPH_SLACK_BOT_TOKEN"); ok {
		if out.Slack == nil {
			out.Slack = map[string]agentsettings.EnvManagedField{}
		}
		out.Slack["bot_token"] = field
	}
	if field, ok := agentsettings.ManagedEnvField(true, "MISTER_MORPH_SLACK_APP_TOKEN"); ok {
		if out.Slack == nil {
			out.Slack = map[string]agentsettings.EnvManagedField{}
		}
		out.Slack["app_token"] = field
	}
	if field, ok := agentsettings.ManagedEnvField(true, "MISTER_MORPH_LINE_CHANNEL_ACCESS_TOKEN"); ok {
		out.Line = map[string]agentsettings.EnvManagedField{"channel_access_token": field}
	}
	if field, ok := agentsettings.ManagedEnvField(true, "MISTER_MORPH_LINE_CHANNEL_SECRET"); ok {
		if out.Line == nil {
			out.Line = map[string]agentsettings.EnvManagedField{}
		}
		out.Line["channel_secret"] = field
	}
	if field, ok := agentsettings.ManagedEnvField(false, "MISTER_MORPH_LARK_APP_ID"); ok {
		out.Lark = map[string]agentsettings.EnvManagedField{"app_id": field}
	}
	if field, ok := agentsettings.ManagedEnvField(true, "MISTER_MORPH_LARK_APP_SECRET"); ok {
		if out.Lark == nil {
			out.Lark = map[string]agentsettings.EnvManagedField{}
		}
		out.Lark["app_secret"] = field
	}
	return out
}
