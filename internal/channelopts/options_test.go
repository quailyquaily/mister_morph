package channelopts

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
)

type stubConfigReader map[string]any

func (s stubConfigReader) GetStringSlice(key string) []string {
	if v, ok := s[key].([]string); ok {
		return append([]string(nil), v...)
	}
	return nil
}
func (s stubConfigReader) GetString(key string) string {
	if v, ok := s[key].(string); ok {
		return v
	}
	return ""
}
func (s stubConfigReader) GetFloat64(key string) float64 {
	if v, ok := s[key].(float64); ok {
		return v
	}
	return 0
}
func (s stubConfigReader) GetDuration(key string) time.Duration {
	if v, ok := s[key].(time.Duration); ok {
		return v
	}
	return 0
}
func (s stubConfigReader) GetInt(key string) int {
	if v, ok := s[key].(int); ok {
		return v
	}
	return 0
}
func (s stubConfigReader) GetInt64(key string) int64 {
	if v, ok := s[key].(int64); ok {
		return v
	}
	return 0
}
func (s stubConfigReader) GetBool(key string) bool {
	if v, ok := s[key].(bool); ok {
		return v
	}
	return false
}

func TestParseTelegramAllowedChatIDs(t *testing.T) {
	got, err := ParseTelegramAllowedChatIDs([]string{" 1 ", "", "-100", "1"})
	if err != nil {
		t.Fatalf("ParseTelegramAllowedChatIDs() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (%#v)", len(got), got)
	}
	if got[0] != 1 || got[1] != -100 {
		t.Fatalf("got = %#v, want [1 -100]", got)
	}
}

func TestResolveServeListenPrefersChannelSpecific(t *testing.T) {
	cfg := stubConfigReader{
		"telegram.serve_listen": "127.0.0.1:19999",
	}

	got := resolveServeListen(cfg, "telegram.serve_listen", defaultTelegramServeListen)
	if got != "127.0.0.1:19999" {
		t.Fatalf("resolveServeListen() = %q, want %q", got, "127.0.0.1:19999")
	}
}

func TestResolveServeListenFallsBackToChannelDefault(t *testing.T) {
	cfg := stubConfigReader{
		"telegram.serve_listen": "",
	}

	got := resolveServeListen(cfg, "telegram.serve_listen", defaultTelegramServeListen)
	if got != defaultTelegramServeListen {
		t.Fatalf("resolveServeListen() = %q, want %q", got, defaultTelegramServeListen)
	}
}

func TestParseTelegramAllowedChatIDsInvalid(t *testing.T) {
	if _, err := ParseTelegramAllowedChatIDs([]string{"abc"}); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestBuildTelegramRunOptionsTaskTimeoutFallback(t *testing.T) {
	opts, err := BuildTelegramRunOptions(
		TelegramConfig{
			AllowedChatIDsRaw:                    []string{"100"},
			TaskTimeout:                          0,
			GlobalTaskTimeout:                    2 * time.Minute,
			PollTimeout:                          30 * time.Second,
			MaxConcurrency:                       3,
			AgentLimits:                          agent.Limits{ToolRepeatLimit: 9},
			EngineToolsConfig:                    agent.EngineToolsConfig{SpawnEnabled: false},
			DefaultGroupTriggerMode:              "smart",
			DefaultAddressingConfidenceThreshold: 0.6,
			DefaultAddressingInterjectThreshold:  0.6,
			MultimodalImageSources:               []string{"telegram"},
		},
		TelegramInput{
			BotToken:    "token",
			TaskTimeout: 0,
		},
	)
	if err != nil {
		t.Fatalf("BuildTelegramRunOptions() error = %v", err)
	}
	if opts.TaskTimeout != 2*time.Minute {
		t.Fatalf("task timeout = %v, want 2m", opts.TaskTimeout)
	}
	if len(opts.AllowedChatIDs) != 1 || opts.AllowedChatIDs[0] != 100 {
		t.Fatalf("allowed chat ids = %#v, want [100]", opts.AllowedChatIDs)
	}
	if opts.AgentLimits.ToolRepeatLimit != 9 {
		t.Fatalf("agent tool repeat limit = %d, want 9", opts.AgentLimits.ToolRepeatLimit)
	}
	if opts.EngineToolsConfig.SpawnEnabled {
		t.Fatalf("spawn tool should remain disabled")
	}
}

func TestBuildSlackRunOptionsTaskTimeoutFallback(t *testing.T) {
	opts := BuildSlackRunOptions(
		SlackConfig{
			TaskTimeout:                          0,
			GlobalTaskTimeout:                    3 * time.Minute,
			MaxConcurrency:                       3,
			FileCacheDir:                         "/tmp/morph-cache",
			AgentLimits:                          agent.Limits{ToolRepeatLimit: 11},
			EngineToolsConfig:                    agent.EngineToolsConfig{SpawnEnabled: true},
			DefaultGroupTriggerMode:              "smart",
			DefaultAddressingConfidenceThreshold: 0.6,
			DefaultAddressingInterjectThreshold:  0.6,
			MemoryEnabled:                        true,
			MemoryShortTermDays:                  9,
			MemoryInjectionEnabled:               true,
			MemoryInjectionMaxItems:              33,
		},
		SlackInput{
			BotToken:    "xoxb-1",
			AppToken:    "xapp-1",
			TaskTimeout: 0,
		},
	)
	if opts.TaskTimeout != 3*time.Minute {
		t.Fatalf("task timeout = %v, want 3m", opts.TaskTimeout)
	}
	if opts.AgentLimits.ToolRepeatLimit != 11 {
		t.Fatalf("agent tool repeat limit = %d, want 11", opts.AgentLimits.ToolRepeatLimit)
	}
	if opts.FileCacheDir != "/tmp/morph-cache" {
		t.Fatalf("file cache dir = %q, want %q", opts.FileCacheDir, "/tmp/morph-cache")
	}
	if !opts.EngineToolsConfig.SpawnEnabled {
		t.Fatalf("spawn tool should remain enabled")
	}
	if !opts.MemoryEnabled || opts.MemoryShortTermDays != 9 || !opts.MemoryInjectionEnabled || opts.MemoryInjectionMaxItems != 33 {
		t.Fatalf("memory options mismatch: %#v", opts)
	}
}

func TestHeartbeatConfigFromReader(t *testing.T) {
	cfg := HeartbeatConfigFromReader(stubConfigReader{
		"heartbeat.enabled":  true,
		"heartbeat.interval": 15 * time.Minute,
	})
	if !cfg.Enabled {
		t.Fatalf("enabled = false, want true")
	}
	if cfg.Interval != 15*time.Minute {
		t.Fatalf("interval = %v, want 15m", cfg.Interval)
	}
}

func TestTelegramConfigFromReaderImageSources(t *testing.T) {
	cfg := TelegramConfigFromReader(stubConfigReader{
		"multimodal.image.sources": []string{" telegram ", "slack"},
		"tools.spawn.enabled":      true,
	})
	if len(cfg.MultimodalImageSources) != 2 {
		t.Fatalf("MultimodalImageSources len = %d, want 2", len(cfg.MultimodalImageSources))
	}
	if !cfg.EngineToolsConfig.SpawnEnabled {
		t.Fatalf("cfg.EngineToolsConfig.SpawnEnabled = false, want true")
	}
}

func TestBuildTelegramRunOptionsImageRecognitionEnabledBySource(t *testing.T) {
	opts, err := BuildTelegramRunOptions(
		TelegramConfig{
			AllowedChatIDsRaw:      []string{"100"},
			MultimodalImageSources: []string{" TeLeGrAm "},
		},
		TelegramInput{BotToken: "token"},
	)
	if err != nil {
		t.Fatalf("BuildTelegramRunOptions() error = %v", err)
	}
	if !opts.ImageRecognitionEnabled {
		t.Fatalf("ImageRecognitionEnabled = false, want true when telegram is in sources")
	}
}

func TestBuildTelegramRunOptionsImageRecognitionDisabledWhenSourceMissing(t *testing.T) {
	opts, err := BuildTelegramRunOptions(
		TelegramConfig{
			AllowedChatIDsRaw:      []string{"100"},
			MultimodalImageSources: []string{"slack"},
		},
		TelegramInput{BotToken: "token"},
	)
	if err != nil {
		t.Fatalf("BuildTelegramRunOptions() error = %v", err)
	}
	if opts.ImageRecognitionEnabled {
		t.Fatalf("ImageRecognitionEnabled = true, want false when telegram is not in sources")
	}
}

func TestLineConfigFromReaderAllowedGroupIDs(t *testing.T) {
	cfg := LineConfigFromReader(stubConfigReader{
		"line.allowed_group_ids": []string{"g1", "g2"},
	})
	if len(cfg.AllowedGroupIDsRaw) != 2 {
		t.Fatalf("AllowedGroupIDsRaw len = %d, want 2", len(cfg.AllowedGroupIDsRaw))
	}
}

func TestBuildLineRunOptionsTaskTimeoutFallback(t *testing.T) {
	opts := BuildLineRunOptions(
		LineConfig{
			AllowedGroupIDsRaw:                   []string{"groupA"},
			TaskTimeout:                          0,
			GlobalTaskTimeout:                    4 * time.Minute,
			MaxConcurrency:                       3,
			FileCacheDir:                         "/tmp/morph-cache",
			DefaultGroupTriggerMode:              "smart",
			DefaultAddressingConfidenceThreshold: 0.6,
			DefaultAddressingInterjectThreshold:  0.6,
			AgentLimits:                          agent.Limits{ToolRepeatLimit: 7},
			MultimodalImageSources:               []string{"line"},
		},
		LineInput{
			ChannelAccessToken: "token",
			ChannelSecret:      "secret",
			TaskTimeout:        0,
		},
	)
	if opts.TaskTimeout != 4*time.Minute {
		t.Fatalf("task timeout = %v, want 4m", opts.TaskTimeout)
	}
	if len(opts.AllowedGroupIDs) != 1 || opts.AllowedGroupIDs[0] != "groupA" {
		t.Fatalf("allowed groups = %#v, want [groupA]", opts.AllowedGroupIDs)
	}
	if opts.AgentLimits.ToolRepeatLimit != 7 {
		t.Fatalf("agent tool repeat limit = %d, want 7", opts.AgentLimits.ToolRepeatLimit)
	}
	if !opts.ImageRecognitionEnabled {
		t.Fatalf("ImageRecognitionEnabled = false, want true when line is in sources")
	}
	if opts.FileCacheDir != "/tmp/morph-cache" {
		t.Fatalf("file cache dir = %q, want %q", opts.FileCacheDir, "/tmp/morph-cache")
	}
}

func TestBuildLineRunOptionsInputOverridesAndDedupesGroups(t *testing.T) {
	opts := BuildLineRunOptions(
		LineConfig{
			AllowedGroupIDsRaw: []string{"groupA"},
		},
		LineInput{
			AllowedGroupIDs: []string{" groupB ", "groupB", "groupC"},
		},
	)
	if len(opts.AllowedGroupIDs) != 2 || opts.AllowedGroupIDs[0] != "groupB" || opts.AllowedGroupIDs[1] != "groupC" {
		t.Fatalf("allowed groups = %#v, want [groupB groupC]", opts.AllowedGroupIDs)
	}
}

func TestLarkConfigFromReaderAllowedChatIDs(t *testing.T) {
	cfg := LarkConfigFromReader(stubConfigReader{
		"lark.allowed_chat_ids": []string{"oc_1", "oc_2"},
	})
	if len(cfg.AllowedChatIDs) != 2 {
		t.Fatalf("AllowedChatIDs len = %d, want 2", len(cfg.AllowedChatIDs))
	}
}

func TestBuildLarkRunOptionsTaskTimeoutFallback(t *testing.T) {
	opts := BuildLarkRunOptions(
		LarkConfig{
			AllowedChatIDs:                       []string{"oc_groupA"},
			TaskTimeout:                          0,
			GlobalTaskTimeout:                    5 * time.Minute,
			MaxConcurrency:                       3,
			DefaultGroupTriggerMode:              "smart",
			DefaultAddressingConfidenceThreshold: 0.6,
			DefaultAddressingInterjectThreshold:  0.6,
			AgentLimits:                          agent.Limits{ToolRepeatLimit: 13},
		},
		LarkInput{
			AppID:       "cli_xxx",
			AppSecret:   "secret",
			TaskTimeout: 0,
		},
	)
	if opts.TaskTimeout != 5*time.Minute {
		t.Fatalf("task timeout = %v, want 5m", opts.TaskTimeout)
	}
	if len(opts.AllowedChatIDs) != 1 || opts.AllowedChatIDs[0] != "oc_groupA" {
		t.Fatalf("allowed chats = %#v, want [oc_groupA]", opts.AllowedChatIDs)
	}
	if opts.AgentLimits.ToolRepeatLimit != 13 {
		t.Fatalf("agent tool repeat limit = %d, want 13", opts.AgentLimits.ToolRepeatLimit)
	}
}

func TestBuildLarkRunOptionsInputOverridesAndDedupesChats(t *testing.T) {
	opts := BuildLarkRunOptions(
		LarkConfig{
			AllowedChatIDs: []string{"oc_groupA"},
		},
		LarkInput{
			AllowedChatIDs: []string{" oc_groupB ", "oc_groupB", "oc_groupC"},
		},
	)
	if len(opts.AllowedChatIDs) != 2 || opts.AllowedChatIDs[0] != "oc_groupB" || opts.AllowedChatIDs[1] != "oc_groupC" {
		t.Fatalf("allowed chats = %#v, want [oc_groupB oc_groupC]", opts.AllowedChatIDs)
	}
}
