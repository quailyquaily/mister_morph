package consolecmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestReadConsoleSettings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"console:\n  managed_runtimes: [telegram, slack]\n"+
			"telegram:\n  bot_token: tg-token\n  allowed_chat_ids: [\"123\", \"456\"]\n  group_trigger_mode: talkative\n"+
			"slack:\n  bot_token: xoxb-bot\n  app_token: xapp-app\n  allowed_team_ids: [\"T123\"]\n  allowed_channel_ids: [\"C123\"]\n  group_trigger_mode: strict\n"+
			"guard:\n  enabled: false\n  network:\n    url_fetch:\n      allowed_url_prefixes: [\"https://api.openai.com\"]\n      deny_private_ips: false\n      follow_redirects: true\n      allow_proxy: true\n  redaction:\n    enabled: false\n  approvals:\n    enabled: true\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := readConsoleSettings(configPath)
	if err != nil {
		t.Fatalf("readConsoleSettings() error = %v", err)
	}
	if len(got.ManagedRuntimes) != 2 || got.ManagedRuntimes[0] != "telegram" || got.ManagedRuntimes[1] != "slack" {
		t.Fatalf("got.ManagedRuntimes = %#v, want [telegram slack]", got.ManagedRuntimes)
	}
	if got.Telegram.BotToken != "tg-token" || got.Telegram.GroupTriggerMode != consoleGroupTriggerTalkative {
		t.Fatalf("telegram = %#v", got.Telegram)
	}
	if len(got.Telegram.AllowedChatIDs) != 2 || got.Telegram.AllowedChatIDs[0] != "123" || got.Telegram.AllowedChatIDs[1] != "456" {
		t.Fatalf("telegram allowed chats = %#v", got.Telegram.AllowedChatIDs)
	}
	if got.Slack.BotToken != "xoxb-bot" || got.Slack.AppToken != "xapp-app" || got.Slack.GroupTriggerMode != consoleGroupTriggerStrict {
		t.Fatalf("slack = %#v", got.Slack)
	}
	if got.Guard.Enabled {
		t.Fatalf("guard.enabled = true, want false")
	}
	if len(got.Guard.Network.URLFetch.AllowedURLPrefixes) != 1 || got.Guard.Network.URLFetch.AllowedURLPrefixes[0] != "https://api.openai.com" {
		t.Fatalf("guard.allowed_url_prefixes = %#v", got.Guard.Network.URLFetch.AllowedURLPrefixes)
	}
	if got.Guard.Network.URLFetch.DenyPrivateIPs || !got.Guard.Network.URLFetch.FollowRedirects || !got.Guard.Network.URLFetch.AllowProxy {
		t.Fatalf("guard.network = %#v", got.Guard.Network.URLFetch)
	}
	if got.Guard.Redaction.Enabled || !got.Guard.Approvals.Enabled {
		t.Fatalf("guard redaction/approvals = %#v / %#v", got.Guard.Redaction, got.Guard.Approvals)
	}
}

func TestWriteConsoleSettingsPreservesOtherConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"console:\n  listen: 127.0.0.1:9080\n"+
			"llm:\n  provider: openai\n  model: gpt-5.2\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	serialized, err := writeConsoleSettings(configPath, consoleSettingsPayload{
		ManagedRuntimes: []string{"telegram"},
		Telegram: consoleTelegramSettingsPayload{
			BotToken:         "tg-token",
			AllowedChatIDs:   []string{"123", "456"},
			GroupTriggerMode: consoleGroupTriggerTalkative,
		},
		Slack: consoleSlackSettingsPayload{
			BotToken:          "xoxb-bot",
			AppToken:          "xapp-app",
			AllowedTeamIDs:    []string{"T123"},
			AllowedChannelIDs: []string{"C123"},
			GroupTriggerMode:  consoleGroupTriggerStrict,
		},
		Guard: consoleGuardSettingsPayload{
			Enabled: true,
			Network: consoleGuardNetworkSettingsPayload{
				URLFetch: consoleGuardURLFetchSettingsPayload{
					AllowedURLPrefixes: []string{"https://api.openai.com", "https://example.com"},
					DenyPrivateIPs:     true,
					FollowRedirects:    false,
					AllowProxy:         false,
				},
			},
			Redaction: consoleGuardRedactionSettingsPayload{Enabled: true},
			Approvals: consoleGuardApprovalsSettingsPayload{Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("writeConsoleSettings() error = %v", err)
	}
	out := string(serialized)
	if !strings.Contains(out, "listen: 127.0.0.1:9080") || !strings.Contains(out, "provider: openai") {
		t.Fatalf("serialized config lost existing settings: %s", out)
	}
	if !strings.Contains(out, "bot_token: tg-token") || !strings.Contains(out, "app_token: xapp-app") {
		t.Fatalf("serialized config missing channel tokens: %s", out)
	}
	if !strings.Contains(out, "group_trigger_mode: talkative") || !strings.Contains(out, "group_trigger_mode: strict") {
		t.Fatalf("serialized config missing trigger modes: %s", out)
	}
	if !strings.Contains(out, "guard:\n") || !strings.Contains(out, "allowed_url_prefixes:\n") || !strings.Contains(out, "enabled: true") {
		t.Fatalf("serialized config missing guard settings: %s", out)
	}
}

func TestHandleConsoleSettingsPut(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"console:\n  managed_runtimes: [telegram]\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	prevConsole, hadConsole := viper.Get("console"), viper.IsSet("console")
	prevTelegram, hadTelegram := viper.Get("telegram"), viper.IsSet("telegram")
	prevSlack, hadSlack := viper.Get("slack"), viper.IsSet("slack")
	prevGuard, hadGuard := viper.Get("guard"), viper.IsSet("guard")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadConsole {
			viper.Set("console", prevConsole)
		} else {
			viper.Set("console", nil)
		}
		if hadTelegram {
			viper.Set("telegram", prevTelegram)
		} else {
			viper.Set("telegram", nil)
		}
		if hadSlack {
			viper.Set("slack", prevSlack)
		} else {
			viper.Set("slack", nil)
		}
		if hadGuard {
			viper.Set("guard", prevGuard)
		} else {
			viper.Set("guard", nil)
		}
	})

	body := bytes.NewBufferString(`{
		"managed_runtimes":["slack","telegram","slack"],
		"telegram":{"bot_token":"tg-token","allowed_chat_ids":["123","456"],"group_trigger_mode":"talkative"},
		"slack":{"bot_token":"xoxb-bot","app_token":"xapp-app","allowed_team_ids":["T123"],"allowed_channel_ids":["C123"],"group_trigger_mode":"strict"},
		"guard":{"enabled":true,"network":{"url_fetch":{"allowed_url_prefixes":["https://api.openai.com","https://example.com"],"deny_private_ips":true,"follow_redirects":false,"allow_proxy":false}},"redaction":{"enabled":true},"approvals":{"enabled":true}}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/console", body)
	rec := httptest.NewRecorder()

	(&server{managed: newManagedRuntimeSupervisor(nil, false, false)}).handleConsoleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	serialized := string(raw)
	if !strings.Contains(serialized, "- slack") || !strings.Contains(serialized, "- telegram") {
		t.Fatalf("config missing managed runtime update: %s", serialized)
	}
	if !strings.Contains(serialized, "bot_token: tg-token") || !strings.Contains(serialized, "app_token: xapp-app") {
		t.Fatalf("config missing channel token update: %s", serialized)
	}
	if !strings.Contains(serialized, "allowed_url_prefixes:") || !strings.Contains(serialized, "https://api.openai.com") {
		t.Fatalf("config missing guard update: %s", serialized)
	}
	var payload struct {
		OK              bool                           `json:"ok"`
		ManagedRuntimes []string                       `json:"managed_runtimes"`
		Telegram        consoleTelegramSettingsPayload `json:"telegram"`
		Slack           consoleSlackSettingsPayload    `json:"slack"`
		Guard           consoleGuardSettingsPayload    `json:"guard"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.OK {
		t.Fatalf("payload.OK = false, want true")
	}
	if len(payload.ManagedRuntimes) != 2 || payload.ManagedRuntimes[0] != "slack" || payload.ManagedRuntimes[1] != "telegram" {
		t.Fatalf("payload.ManagedRuntimes = %#v, want [slack telegram]", payload.ManagedRuntimes)
	}
	if payload.Telegram.BotToken != "tg-token" || payload.Slack.AppToken != "xapp-app" {
		t.Fatalf("payload tokens not returned: telegram=%#v slack=%#v", payload.Telegram, payload.Slack)
	}
	if !payload.Guard.Enabled || !payload.Guard.Redaction.Enabled || !payload.Guard.Approvals.Enabled {
		t.Fatalf("payload guard not returned: %#v", payload.Guard)
	}
}

func TestHandleConsoleSettingsPutPartialTelegramUpdatePreservesSlack(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"console:\n  managed_runtimes: [telegram, slack]\n"+
			"telegram:\n  bot_token: old-tg\n  allowed_chat_ids: [\"123\"]\n  group_trigger_mode: smart\n"+
			"slack:\n  bot_token: old-bot\n  app_token: old-app\n  allowed_team_ids: [\"T123\"]\n  allowed_channel_ids: [\"C123\"]\n  group_trigger_mode: strict\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/console", bytes.NewBufferString(`{
		"telegram":{"bot_token":"new-tg","allowed_chat_ids":["456"],"group_trigger_mode":"talkative"}
	}`))
	rec := httptest.NewRecorder()

	(&server{managed: newManagedRuntimeSupervisor(nil, false, false)}).handleConsoleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	got, err := readConsoleSettings(configPath)
	if err != nil {
		t.Fatalf("readConsoleSettings() error = %v", err)
	}
	if got.Telegram.BotToken != "new-tg" || got.Telegram.GroupTriggerMode != consoleGroupTriggerTalkative {
		t.Fatalf("telegram = %#v", got.Telegram)
	}
	if got.Slack.BotToken != "old-bot" || got.Slack.AppToken != "old-app" || got.Slack.GroupTriggerMode != consoleGroupTriggerStrict {
		t.Fatalf("slack should be preserved, got %#v", got.Slack)
	}
}

func TestHandleConsoleSettingsPutPartialGuardUpdatePreservesChannels(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"console:\n  managed_runtimes: [telegram, slack]\n"+
			"telegram:\n  bot_token: old-tg\n"+
			"slack:\n  bot_token: old-bot\n  app_token: old-app\n"+
			"guard:\n  enabled: true\n  network:\n    url_fetch:\n      allowed_url_prefixes: [\"https://\"]\n      deny_private_ips: true\n      follow_redirects: false\n      allow_proxy: false\n  redaction:\n    enabled: true\n  approvals:\n    enabled: false\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	req := httptest.NewRequest(http.MethodPut, "/api/settings/console", bytes.NewBufferString(`{
		"guard":{"network":{"url_fetch":{"follow_redirects":true}},"approvals":{"enabled":true}}
	}`))
	rec := httptest.NewRecorder()

	(&server{managed: newManagedRuntimeSupervisor(nil, false, false)}).handleConsoleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	got, err := readConsoleSettings(configPath)
	if err != nil {
		t.Fatalf("readConsoleSettings() error = %v", err)
	}
	if got.Telegram.BotToken != "old-tg" || got.Slack.BotToken != "old-bot" || got.Slack.AppToken != "old-app" {
		t.Fatalf("channels should be preserved, got telegram=%#v slack=%#v", got.Telegram, got.Slack)
	}
	if !got.Guard.Enabled || !got.Guard.Network.URLFetch.DenyPrivateIPs || !got.Guard.Network.URLFetch.FollowRedirects || got.Guard.Network.URLFetch.AllowProxy {
		t.Fatalf("guard.network = %#v", got.Guard.Network.URLFetch)
	}
	if !got.Guard.Redaction.Enabled || !got.Guard.Approvals.Enabled {
		t.Fatalf("guard redaction/approvals = %#v / %#v", got.Guard.Redaction, got.Guard.Approvals)
	}
}

func TestHandleConsoleSettingsGetMarksEnvManagedTokens(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"telegram:\n  bot_token: ${MISTER_MORPH_TELEGRAM_BOT_TOKEN}\n"+
			"slack:\n  bot_token: ${MISTER_MORPH_SLACK_BOT_TOKEN}\n  app_token: ${MISTER_MORPH_SLACK_APP_TOKEN}\n",
	), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("MISTER_MORPH_TELEGRAM_BOT_TOKEN", "tg-env")
	t.Setenv("MISTER_MORPH_SLACK_BOT_TOKEN", "xoxb-env")
	t.Setenv("MISTER_MORPH_SLACK_APP_TOKEN", "xapp-env")

	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings/console", nil)
	rec := httptest.NewRecorder()
	(&server{}).handleConsoleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Telegram struct {
			BotToken string `json:"bot_token"`
		} `json:"telegram"`
		Slack struct {
			BotToken string `json:"bot_token"`
			AppToken string `json:"app_token"`
		} `json:"slack"`
		EnvManaged consoleSettingsEnvManagedPayload `json:"env_managed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Telegram.BotToken != "" || payload.Slack.BotToken != "" || payload.Slack.AppToken != "" {
		t.Fatalf("expected env-managed tokens to be hidden, got telegram=%q slack=%q/%q", payload.Telegram.BotToken, payload.Slack.BotToken, payload.Slack.AppToken)
	}
	if got := payload.EnvManaged.Telegram["bot_token"].EnvName; got != "MISTER_MORPH_TELEGRAM_BOT_TOKEN" {
		t.Fatalf("telegram env = %q", got)
	}
	if got := payload.EnvManaged.Slack["bot_token"].EnvName; got != "MISTER_MORPH_SLACK_BOT_TOKEN" {
		t.Fatalf("slack bot env = %q", got)
	}
	if got := payload.EnvManaged.Slack["app_token"].EnvName; got != "MISTER_MORPH_SLACK_APP_TOKEN" {
		t.Fatalf("slack app env = %q", got)
	}
	if got := payload.EnvManaged.Telegram["bot_token"].RawValue; got != "${MISTER_MORPH_TELEGRAM_BOT_TOKEN}" {
		t.Fatalf("telegram raw value = %q", got)
	}
}

func TestHandleConsoleSettingsPutRejectsInvalidRuntime(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/settings/console", bytes.NewBufferString(`{"managed_runtimes":["line"]}`))
	rec := httptest.NewRecorder()

	(&server{}).handleConsoleSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleConsoleSettingsPutRejectsInvalidTelegramChatID(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/settings/console", bytes.NewBufferString(`{
		"telegram":{"allowed_chat_ids":["abc"]}
	}`))
	rec := httptest.NewRecorder()

	(&server{}).handleConsoleSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
