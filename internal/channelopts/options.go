package channelopts

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	larkruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/lark"
	lineruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/line"
	slackruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/slack"
	telegramruntime "github.com/quailyquaily/mistermorph/internal/channelruntime/telegram"
	"github.com/spf13/viper"
)

type ConfigReader interface {
	GetStringSlice(string) []string
	GetString(string) string
	GetFloat64(string) float64
	GetDuration(string) time.Duration
	GetInt(string) int
	GetInt64(string) int64
	GetBool(string) bool
}

const (
	defaultTelegramServeListen = "127.0.0.1:8787"
	defaultSlackServeListen    = "127.0.0.1:8788"
	defaultLineServeListen     = "127.0.0.1:8789"
	defaultLarkServeListen     = "127.0.0.1:8790"
)

type TelegramConfig struct {
	AllowedChatIDsRaw                    []string
	DefaultGroupTriggerMode              string
	DefaultAddressingConfidenceThreshold float64
	DefaultAddressingInterjectThreshold  float64
	PollTimeout                          time.Duration
	TaskTimeout                          time.Duration
	GlobalTaskTimeout                    time.Duration
	MaxConcurrency                       int
	FileCacheDir                         string
	ServerListen                         string
	ServerAuthToken                      string
	ServerMaxQueue                       int
	BusMaxInFlight                       int
	RequestTimeout                       time.Duration
	AgentLimits                          agent.Limits
	FileCacheMaxAge                      time.Duration
	FileCacheMaxFiles                    int
	FileCacheMaxTotalBytes               int64
	MemoryEnabled                        bool
	MemoryShortTermDays                  int
	MemoryInjectionEnabled               bool
	MemoryInjectionMaxItems              int
	MultimodalImageSources               []string
}

type TelegramInput struct {
	BotToken                      string
	AllowedChatIDs                []int64
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	PollTimeout                   time.Duration
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	FileCacheDir                  string
	Hooks                         telegramruntime.Hooks
	InspectPrompt                 bool
	InspectRequest                bool
}

func TelegramConfigFromReader(r ConfigReader) TelegramConfig {
	if r == nil {
		return TelegramConfig{}
	}
	return TelegramConfig{
		AllowedChatIDsRaw:                    append([]string(nil), r.GetStringSlice("telegram.allowed_chat_ids")...),
		DefaultGroupTriggerMode:              strings.TrimSpace(r.GetString("telegram.group_trigger_mode")),
		DefaultAddressingConfidenceThreshold: r.GetFloat64("telegram.addressing_confidence_threshold"),
		DefaultAddressingInterjectThreshold:  r.GetFloat64("telegram.addressing_interject_threshold"),
		PollTimeout:                          r.GetDuration("telegram.poll_timeout"),
		TaskTimeout:                          r.GetDuration("telegram.task_timeout"),
		GlobalTaskTimeout:                    r.GetDuration("timeout"),
		MaxConcurrency:                       r.GetInt("telegram.max_concurrency"),
		FileCacheDir:                         strings.TrimSpace(r.GetString("file_cache_dir")),
		ServerListen:                         resolveServeListen(r, "telegram.serve_listen", defaultTelegramServeListen),
		ServerAuthToken:                      strings.TrimSpace(r.GetString("server.auth_token")),
		ServerMaxQueue:                       r.GetInt("server.max_queue"),
		BusMaxInFlight:                       r.GetInt("bus.max_inflight"),
		RequestTimeout:                       r.GetDuration("llm.request_timeout"),
		AgentLimits: agent.Limits{
			MaxSteps:        r.GetInt("max_steps"),
			ParseRetries:    r.GetInt("parse_retries"),
			MaxTokenBudget:  r.GetInt("max_token_budget"),
			ToolRepeatLimit: r.GetInt("tool_repeat_limit"),
		},
		FileCacheMaxAge:         r.GetDuration("file_cache.max_age"),
		FileCacheMaxFiles:       r.GetInt("file_cache.max_files"),
		FileCacheMaxTotalBytes:  r.GetInt64("file_cache.max_total_bytes"),
		MemoryEnabled:           r.GetBool("memory.enabled"),
		MemoryShortTermDays:     r.GetInt("memory.short_term_days"),
		MemoryInjectionEnabled:  r.GetBool("memory.injection.enabled"),
		MemoryInjectionMaxItems: r.GetInt("memory.injection.max_items"),
		MultimodalImageSources:  append([]string(nil), r.GetStringSlice("multimodal.image.sources")...),
	}
}

func TelegramConfigFromViper() TelegramConfig {
	return TelegramConfigFromReader(viper.GetViper())
}

type HeartbeatConfig struct {
	Enabled  bool
	Interval time.Duration
}

func HeartbeatConfigFromReader(r ConfigReader) HeartbeatConfig {
	if r == nil {
		return HeartbeatConfig{}
	}
	return HeartbeatConfig{
		Enabled:  r.GetBool("heartbeat.enabled"),
		Interval: r.GetDuration("heartbeat.interval"),
	}
}

func HeartbeatConfigFromViper() HeartbeatConfig {
	return HeartbeatConfigFromReader(viper.GetViper())
}

func BuildTelegramRunOptions(cfg TelegramConfig, in TelegramInput) (telegramruntime.RunOptions, error) {
	allowedChatIDs := append([]int64(nil), in.AllowedChatIDs...)
	if len(allowedChatIDs) == 0 {
		ids, err := ParseTelegramAllowedChatIDs(cfg.AllowedChatIDsRaw)
		if err != nil {
			return telegramruntime.RunOptions{}, err
		}
		allowedChatIDs = ids
	}

	groupTriggerMode := strings.TrimSpace(in.GroupTriggerMode)
	if groupTriggerMode == "" {
		groupTriggerMode = strings.TrimSpace(cfg.DefaultGroupTriggerMode)
	}
	addressingConfidenceThreshold := in.AddressingConfidenceThreshold
	if addressingConfidenceThreshold <= 0 {
		addressingConfidenceThreshold = cfg.DefaultAddressingConfidenceThreshold
	}
	addressingInterjectThreshold := in.AddressingInterjectThreshold
	if addressingInterjectThreshold <= 0 {
		addressingInterjectThreshold = cfg.DefaultAddressingInterjectThreshold
	}
	pollTimeout := in.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = cfg.PollTimeout
	}
	taskTimeout := in.TaskTimeout
	if taskTimeout <= 0 {
		taskTimeout = cfg.TaskTimeout
	}
	if taskTimeout <= 0 {
		taskTimeout = cfg.GlobalTaskTimeout
	}
	maxConcurrency := in.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = cfg.MaxConcurrency
	}
	fileCacheDir := strings.TrimSpace(in.FileCacheDir)
	if fileCacheDir == "" {
		fileCacheDir = strings.TrimSpace(cfg.FileCacheDir)
	}
	serverListen := normalizeServerListen(cfg.ServerListen)
	imageRecognitionEnabled := sourceEnabled(cfg.MultimodalImageSources, "telegram")

	return telegramruntime.RunOptions{
		BotToken:                      strings.TrimSpace(in.BotToken),
		AllowedChatIDs:                allowedChatIDs,
		GroupTriggerMode:              groupTriggerMode,
		AddressingConfidenceThreshold: addressingConfidenceThreshold,
		AddressingInterjectThreshold:  addressingInterjectThreshold,
		PollTimeout:                   pollTimeout,
		TaskTimeout:                   taskTimeout,
		MaxConcurrency:                maxConcurrency,
		FileCacheDir:                  fileCacheDir,
		Server: telegramruntime.ServerOptions{
			Listen:    serverListen,
			AuthToken: cfg.ServerAuthToken,
			MaxQueue:  cfg.ServerMaxQueue,
		},
		BusMaxInFlight:          cfg.BusMaxInFlight,
		RequestTimeout:          cfg.RequestTimeout,
		AgentLimits:             cfg.AgentLimits,
		FileCacheMaxAge:         cfg.FileCacheMaxAge,
		FileCacheMaxFiles:       cfg.FileCacheMaxFiles,
		FileCacheMaxTotalBytes:  cfg.FileCacheMaxTotalBytes,
		MemoryEnabled:           cfg.MemoryEnabled,
		MemoryShortTermDays:     cfg.MemoryShortTermDays,
		MemoryInjectionEnabled:  cfg.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: cfg.MemoryInjectionMaxItems,
		ImageRecognitionEnabled: imageRecognitionEnabled,
		Hooks:                   in.Hooks,
		InspectPrompt:           in.InspectPrompt,
		InspectRequest:          in.InspectRequest,
	}, nil
}

func ParseTelegramAllowedChatIDs(values []string) ([]int64, error) {
	if len(values) == 0 {
		return []int64{}, nil
	}
	out := make([]int64, 0, len(values))
	seen := map[int64]struct{}{}
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram.allowed_chat_ids entry %q: %w", raw, err)
		}
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return []int64{}, nil
	}
	return out, nil
}

type SlackConfig struct {
	AllowedTeamIDs                       []string
	AllowedChannelIDs                    []string
	DefaultGroupTriggerMode              string
	DefaultAddressingConfidenceThreshold float64
	DefaultAddressingInterjectThreshold  float64
	TaskTimeout                          time.Duration
	GlobalTaskTimeout                    time.Duration
	MaxConcurrency                       int
	FileCacheDir                         string
	ServerListen                         string
	ServerAuthToken                      string
	ServerMaxQueue                       int
	BaseURL                              string
	BusMaxInFlight                       int
	RequestTimeout                       time.Duration
	AgentLimits                          agent.Limits
	MemoryEnabled                        bool
	MemoryShortTermDays                  int
	MemoryInjectionEnabled               bool
	MemoryInjectionMaxItems              int
}

type SlackInput struct {
	BotToken                      string
	AppToken                      string
	AllowedTeamIDs                []string
	AllowedChannelIDs             []string
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	BaseURL                       string
	Hooks                         slackruntime.Hooks
	InspectPrompt                 bool
	InspectRequest                bool
}

func SlackConfigFromReader(r ConfigReader) SlackConfig {
	if r == nil {
		return SlackConfig{}
	}
	return SlackConfig{
		AllowedTeamIDs:                       append([]string(nil), r.GetStringSlice("slack.allowed_team_ids")...),
		AllowedChannelIDs:                    append([]string(nil), r.GetStringSlice("slack.allowed_channel_ids")...),
		DefaultGroupTriggerMode:              strings.TrimSpace(r.GetString("slack.group_trigger_mode")),
		DefaultAddressingConfidenceThreshold: r.GetFloat64("slack.addressing_confidence_threshold"),
		DefaultAddressingInterjectThreshold:  r.GetFloat64("slack.addressing_interject_threshold"),
		TaskTimeout:                          r.GetDuration("slack.task_timeout"),
		GlobalTaskTimeout:                    r.GetDuration("timeout"),
		MaxConcurrency:                       r.GetInt("slack.max_concurrency"),
		FileCacheDir:                         strings.TrimSpace(r.GetString("file_cache_dir")),
		ServerListen:                         resolveServeListen(r, "slack.serve_listen", defaultSlackServeListen),
		ServerAuthToken:                      strings.TrimSpace(r.GetString("server.auth_token")),
		ServerMaxQueue:                       r.GetInt("server.max_queue"),
		BaseURL:                              strings.TrimSpace(r.GetString("slack.base_url")),
		BusMaxInFlight:                       r.GetInt("bus.max_inflight"),
		RequestTimeout:                       r.GetDuration("llm.request_timeout"),
		AgentLimits: agent.Limits{
			MaxSteps:        r.GetInt("max_steps"),
			ParseRetries:    r.GetInt("parse_retries"),
			MaxTokenBudget:  r.GetInt("max_token_budget"),
			ToolRepeatLimit: r.GetInt("tool_repeat_limit"),
		},
		MemoryEnabled:           r.GetBool("memory.enabled"),
		MemoryShortTermDays:     r.GetInt("memory.short_term_days"),
		MemoryInjectionEnabled:  r.GetBool("memory.injection.enabled"),
		MemoryInjectionMaxItems: r.GetInt("memory.injection.max_items"),
	}
}

func SlackConfigFromViper() SlackConfig {
	return SlackConfigFromReader(viper.GetViper())
}

func BuildSlackRunOptions(cfg SlackConfig, in SlackInput) slackruntime.RunOptions {
	allowedTeamIDs := append([]string(nil), in.AllowedTeamIDs...)
	if len(allowedTeamIDs) == 0 {
		allowedTeamIDs = append([]string(nil), cfg.AllowedTeamIDs...)
	}
	allowedChannelIDs := append([]string(nil), in.AllowedChannelIDs...)
	if len(allowedChannelIDs) == 0 {
		allowedChannelIDs = append([]string(nil), cfg.AllowedChannelIDs...)
	}

	groupTriggerMode := strings.TrimSpace(in.GroupTriggerMode)
	if groupTriggerMode == "" {
		groupTriggerMode = strings.TrimSpace(cfg.DefaultGroupTriggerMode)
	}
	addressingConfidenceThreshold := in.AddressingConfidenceThreshold
	if addressingConfidenceThreshold <= 0 {
		addressingConfidenceThreshold = cfg.DefaultAddressingConfidenceThreshold
	}
	addressingInterjectThreshold := in.AddressingInterjectThreshold
	if addressingInterjectThreshold <= 0 {
		addressingInterjectThreshold = cfg.DefaultAddressingInterjectThreshold
	}
	taskTimeout := in.TaskTimeout
	if taskTimeout <= 0 {
		taskTimeout = cfg.TaskTimeout
	}
	if taskTimeout <= 0 {
		taskTimeout = cfg.GlobalTaskTimeout
	}
	maxConcurrency := in.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = cfg.MaxConcurrency
	}
	fileCacheDir := strings.TrimSpace(cfg.FileCacheDir)
	serverListen := normalizeServerListen(cfg.ServerListen)
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.BaseURL)
	}

	return slackruntime.RunOptions{
		BotToken:                      strings.TrimSpace(in.BotToken),
		AppToken:                      strings.TrimSpace(in.AppToken),
		AllowedTeamIDs:                allowedTeamIDs,
		AllowedChannelIDs:             allowedChannelIDs,
		GroupTriggerMode:              groupTriggerMode,
		AddressingConfidenceThreshold: addressingConfidenceThreshold,
		AddressingInterjectThreshold:  addressingInterjectThreshold,
		TaskTimeout:                   taskTimeout,
		MaxConcurrency:                maxConcurrency,
		FileCacheDir:                  fileCacheDir,
		Server: slackruntime.ServerOptions{
			Listen:    serverListen,
			AuthToken: cfg.ServerAuthToken,
			MaxQueue:  cfg.ServerMaxQueue,
		},
		BaseURL:                 baseURL,
		BusMaxInFlight:          cfg.BusMaxInFlight,
		RequestTimeout:          cfg.RequestTimeout,
		AgentLimits:             cfg.AgentLimits,
		MemoryEnabled:           cfg.MemoryEnabled,
		MemoryShortTermDays:     cfg.MemoryShortTermDays,
		MemoryInjectionEnabled:  cfg.MemoryInjectionEnabled,
		MemoryInjectionMaxItems: cfg.MemoryInjectionMaxItems,
		Hooks:                   in.Hooks,
		InspectPrompt:           in.InspectPrompt,
		InspectRequest:          in.InspectRequest,
	}
}

type LineConfig struct {
	AllowedGroupIDsRaw                   []string
	DefaultGroupTriggerMode              string
	DefaultAddressingConfidenceThreshold float64
	DefaultAddressingInterjectThreshold  float64
	TaskTimeout                          time.Duration
	GlobalTaskTimeout                    time.Duration
	MaxConcurrency                       int
	FileCacheDir                         string
	ServerListen                         string
	ServerAuthToken                      string
	ServerMaxQueue                       int
	BaseURL                              string
	WebhookListen                        string
	WebhookPath                          string
	BusMaxInFlight                       int
	RequestTimeout                       time.Duration
	AgentLimits                          agent.Limits
	MemoryEnabled                        bool
	MemoryShortTermDays                  int
	MemoryInjectionEnabled               bool
	MemoryInjectionMaxItems              int
	MultimodalImageSources               []string
}

type LineInput struct {
	ChannelAccessToken            string
	ChannelSecret                 string
	AllowedGroupIDs               []string
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	BaseURL                       string
	WebhookListen                 string
	WebhookPath                   string
	Hooks                         lineruntime.Hooks
	InspectPrompt                 bool
	InspectRequest                bool
}

type LarkConfig struct {
	AllowedChatIDs                       []string
	DefaultGroupTriggerMode              string
	DefaultAddressingConfidenceThreshold float64
	DefaultAddressingInterjectThreshold  float64
	TaskTimeout                          time.Duration
	GlobalTaskTimeout                    time.Duration
	MaxConcurrency                       int
	ServerListen                         string
	ServerAuthToken                      string
	ServerMaxQueue                       int
	BaseURL                              string
	WebhookListen                        string
	WebhookPath                          string
	VerificationToken                    string
	EncryptKey                           string
	BusMaxInFlight                       int
	RequestTimeout                       time.Duration
	AgentLimits                          agent.Limits
	MemoryEnabled                        bool
	MemoryShortTermDays                  int
	MemoryInjectionEnabled               bool
	MemoryInjectionMaxItems              int
}

type LarkInput struct {
	AppID                         string
	AppSecret                     string
	AllowedChatIDs                []string
	GroupTriggerMode              string
	AddressingConfidenceThreshold float64
	AddressingInterjectThreshold  float64
	TaskTimeout                   time.Duration
	MaxConcurrency                int
	BaseURL                       string
	WebhookListen                 string
	WebhookPath                   string
	VerificationToken             string
	EncryptKey                    string
	Hooks                         larkruntime.Hooks
	InspectPrompt                 bool
	InspectRequest                bool
}

func LineConfigFromReader(r ConfigReader) LineConfig {
	if r == nil {
		return LineConfig{}
	}
	return LineConfig{
		AllowedGroupIDsRaw:                   append([]string(nil), r.GetStringSlice("line.allowed_group_ids")...),
		DefaultGroupTriggerMode:              strings.TrimSpace(r.GetString("line.group_trigger_mode")),
		DefaultAddressingConfidenceThreshold: r.GetFloat64("line.addressing_confidence_threshold"),
		DefaultAddressingInterjectThreshold:  r.GetFloat64("line.addressing_interject_threshold"),
		TaskTimeout:                          r.GetDuration("line.task_timeout"),
		GlobalTaskTimeout:                    r.GetDuration("timeout"),
		MaxConcurrency:                       r.GetInt("line.max_concurrency"),
		FileCacheDir:                         strings.TrimSpace(r.GetString("file_cache_dir")),
		ServerListen:                         resolveServeListen(r, "line.serve_listen", defaultLineServeListen),
		ServerAuthToken:                      strings.TrimSpace(r.GetString("server.auth_token")),
		ServerMaxQueue:                       r.GetInt("server.max_queue"),
		BaseURL:                              strings.TrimSpace(r.GetString("line.base_url")),
		WebhookListen:                        strings.TrimSpace(r.GetString("line.webhook_listen")),
		WebhookPath:                          strings.TrimSpace(r.GetString("line.webhook_path")),
		BusMaxInFlight:                       r.GetInt("bus.max_inflight"),
		RequestTimeout:                       r.GetDuration("llm.request_timeout"),
		AgentLimits: agent.Limits{
			MaxSteps:        r.GetInt("max_steps"),
			ParseRetries:    r.GetInt("parse_retries"),
			MaxTokenBudget:  r.GetInt("max_token_budget"),
			ToolRepeatLimit: r.GetInt("tool_repeat_limit"),
		},
		MemoryEnabled:           r.GetBool("memory.enabled"),
		MemoryShortTermDays:     r.GetInt("memory.short_term_days"),
		MemoryInjectionEnabled:  r.GetBool("memory.injection.enabled"),
		MemoryInjectionMaxItems: r.GetInt("memory.injection.max_items"),
		MultimodalImageSources:  append([]string(nil), r.GetStringSlice("multimodal.image.sources")...),
	}
}

func LineConfigFromViper() LineConfig {
	return LineConfigFromReader(viper.GetViper())
}

func LarkConfigFromReader(r ConfigReader) LarkConfig {
	if r == nil {
		return LarkConfig{}
	}
	return LarkConfig{
		AllowedChatIDs:                       append([]string(nil), r.GetStringSlice("lark.allowed_chat_ids")...),
		DefaultGroupTriggerMode:              strings.TrimSpace(r.GetString("lark.group_trigger_mode")),
		DefaultAddressingConfidenceThreshold: r.GetFloat64("lark.addressing_confidence_threshold"),
		DefaultAddressingInterjectThreshold:  r.GetFloat64("lark.addressing_interject_threshold"),
		TaskTimeout:                          r.GetDuration("lark.task_timeout"),
		GlobalTaskTimeout:                    r.GetDuration("timeout"),
		MaxConcurrency:                       r.GetInt("lark.max_concurrency"),
		ServerListen:                         resolveServeListen(r, "lark.serve_listen", defaultLarkServeListen),
		ServerAuthToken:                      strings.TrimSpace(r.GetString("server.auth_token")),
		ServerMaxQueue:                       r.GetInt("server.max_queue"),
		BaseURL:                              strings.TrimSpace(r.GetString("lark.base_url")),
		WebhookListen:                        strings.TrimSpace(r.GetString("lark.webhook_listen")),
		WebhookPath:                          strings.TrimSpace(r.GetString("lark.webhook_path")),
		VerificationToken:                    strings.TrimSpace(r.GetString("lark.verification_token")),
		EncryptKey:                           strings.TrimSpace(r.GetString("lark.encrypt_key")),
		BusMaxInFlight:                       r.GetInt("bus.max_inflight"),
		RequestTimeout:                       r.GetDuration("llm.request_timeout"),
		AgentLimits: agent.Limits{
			MaxSteps:        r.GetInt("max_steps"),
			ParseRetries:    r.GetInt("parse_retries"),
			MaxTokenBudget:  r.GetInt("max_token_budget"),
			ToolRepeatLimit: r.GetInt("tool_repeat_limit"),
		},
		MemoryEnabled:           r.GetBool("memory.enabled"),
		MemoryShortTermDays:     r.GetInt("memory.short_term_days"),
		MemoryInjectionEnabled:  r.GetBool("memory.injection.enabled"),
		MemoryInjectionMaxItems: r.GetInt("memory.injection.max_items"),
	}
}

func LarkConfigFromViper() LarkConfig {
	return LarkConfigFromReader(viper.GetViper())
}

func BuildLineRunOptions(cfg LineConfig, in LineInput) lineruntime.RunOptions {
	allowedGroupIDs := normalizeTrimmedUniqueStrings(in.AllowedGroupIDs)
	if len(allowedGroupIDs) == 0 {
		allowedGroupIDs = normalizeTrimmedUniqueStrings(cfg.AllowedGroupIDsRaw)
	}

	groupTriggerMode := strings.TrimSpace(in.GroupTriggerMode)
	if groupTriggerMode == "" {
		groupTriggerMode = strings.TrimSpace(cfg.DefaultGroupTriggerMode)
	}
	addressingConfidenceThreshold := in.AddressingConfidenceThreshold
	if addressingConfidenceThreshold <= 0 {
		addressingConfidenceThreshold = cfg.DefaultAddressingConfidenceThreshold
	}
	addressingInterjectThreshold := in.AddressingInterjectThreshold
	if addressingInterjectThreshold <= 0 {
		addressingInterjectThreshold = cfg.DefaultAddressingInterjectThreshold
	}
	taskTimeout := in.TaskTimeout
	if taskTimeout <= 0 {
		taskTimeout = cfg.TaskTimeout
	}
	if taskTimeout <= 0 {
		taskTimeout = cfg.GlobalTaskTimeout
	}
	maxConcurrency := in.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = cfg.MaxConcurrency
	}
	fileCacheDir := strings.TrimSpace(cfg.FileCacheDir)
	serverListen := normalizeServerListen(cfg.ServerListen)
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.BaseURL)
	}
	webhookListen := strings.TrimSpace(in.WebhookListen)
	if webhookListen == "" {
		webhookListen = strings.TrimSpace(cfg.WebhookListen)
	}
	webhookPath := strings.TrimSpace(in.WebhookPath)
	if webhookPath == "" {
		webhookPath = strings.TrimSpace(cfg.WebhookPath)
	}
	imageRecognitionEnabled := sourceEnabled(cfg.MultimodalImageSources, "line")

	return lineruntime.RunOptions{
		ChannelAccessToken:            strings.TrimSpace(in.ChannelAccessToken),
		ChannelSecret:                 strings.TrimSpace(in.ChannelSecret),
		AllowedGroupIDs:               allowedGroupIDs,
		GroupTriggerMode:              groupTriggerMode,
		AddressingConfidenceThreshold: addressingConfidenceThreshold,
		AddressingInterjectThreshold:  addressingInterjectThreshold,
		TaskTimeout:                   taskTimeout,
		MaxConcurrency:                maxConcurrency,
		FileCacheDir:                  fileCacheDir,
		ServerListen:                  serverListen,
		ServerAuthToken:               cfg.ServerAuthToken,
		ServerMaxQueue:                cfg.ServerMaxQueue,
		BaseURL:                       baseURL,
		WebhookListen:                 webhookListen,
		WebhookPath:                   webhookPath,
		BusMaxInFlight:                cfg.BusMaxInFlight,
		RequestTimeout:                cfg.RequestTimeout,
		AgentLimits:                   cfg.AgentLimits,
		MemoryEnabled:                 cfg.MemoryEnabled,
		MemoryShortTermDays:           cfg.MemoryShortTermDays,
		MemoryInjectionEnabled:        cfg.MemoryInjectionEnabled,
		MemoryInjectionMaxItems:       cfg.MemoryInjectionMaxItems,
		ImageRecognitionEnabled:       imageRecognitionEnabled,
		Hooks:                         in.Hooks,
		InspectPrompt:                 in.InspectPrompt,
		InspectRequest:                in.InspectRequest,
	}
}

func BuildLarkRunOptions(cfg LarkConfig, in LarkInput) larkruntime.RunOptions {
	allowedChatIDs := normalizeTrimmedUniqueStrings(in.AllowedChatIDs)
	if len(allowedChatIDs) == 0 {
		allowedChatIDs = normalizeTrimmedUniqueStrings(cfg.AllowedChatIDs)
	}

	groupTriggerMode := strings.TrimSpace(in.GroupTriggerMode)
	if groupTriggerMode == "" {
		groupTriggerMode = strings.TrimSpace(cfg.DefaultGroupTriggerMode)
	}
	addressingConfidenceThreshold := in.AddressingConfidenceThreshold
	if addressingConfidenceThreshold <= 0 {
		addressingConfidenceThreshold = cfg.DefaultAddressingConfidenceThreshold
	}
	addressingInterjectThreshold := in.AddressingInterjectThreshold
	if addressingInterjectThreshold <= 0 {
		addressingInterjectThreshold = cfg.DefaultAddressingInterjectThreshold
	}
	taskTimeout := in.TaskTimeout
	if taskTimeout <= 0 {
		taskTimeout = cfg.TaskTimeout
	}
	if taskTimeout <= 0 {
		taskTimeout = cfg.GlobalTaskTimeout
	}
	maxConcurrency := in.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = cfg.MaxConcurrency
	}
	serverListen := normalizeServerListen(cfg.ServerListen)
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.BaseURL)
	}
	webhookListen := strings.TrimSpace(in.WebhookListen)
	if webhookListen == "" {
		webhookListen = strings.TrimSpace(cfg.WebhookListen)
	}
	webhookPath := strings.TrimSpace(in.WebhookPath)
	if webhookPath == "" {
		webhookPath = strings.TrimSpace(cfg.WebhookPath)
	}
	verificationToken := strings.TrimSpace(in.VerificationToken)
	if verificationToken == "" {
		verificationToken = strings.TrimSpace(cfg.VerificationToken)
	}
	encryptKey := strings.TrimSpace(in.EncryptKey)
	if encryptKey == "" {
		encryptKey = strings.TrimSpace(cfg.EncryptKey)
	}

	return larkruntime.RunOptions{
		AppID:                         strings.TrimSpace(in.AppID),
		AppSecret:                     strings.TrimSpace(in.AppSecret),
		AllowedChatIDs:                allowedChatIDs,
		GroupTriggerMode:              groupTriggerMode,
		AddressingConfidenceThreshold: addressingConfidenceThreshold,
		AddressingInterjectThreshold:  addressingInterjectThreshold,
		TaskTimeout:                   taskTimeout,
		MaxConcurrency:                maxConcurrency,
		ServerListen:                  serverListen,
		ServerAuthToken:               cfg.ServerAuthToken,
		ServerMaxQueue:                cfg.ServerMaxQueue,
		BaseURL:                       baseURL,
		WebhookListen:                 webhookListen,
		WebhookPath:                   webhookPath,
		VerificationToken:             verificationToken,
		EncryptKey:                    encryptKey,
		BusMaxInFlight:                cfg.BusMaxInFlight,
		RequestTimeout:                cfg.RequestTimeout,
		AgentLimits:                   cfg.AgentLimits,
		MemoryEnabled:                 cfg.MemoryEnabled,
		MemoryShortTermDays:           cfg.MemoryShortTermDays,
		MemoryInjectionEnabled:        cfg.MemoryInjectionEnabled,
		MemoryInjectionMaxItems:       cfg.MemoryInjectionMaxItems,
		Hooks:                         in.Hooks,
		InspectPrompt:                 in.InspectPrompt,
		InspectRequest:                in.InspectRequest,
	}
}

func normalizeServerListen(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "127.0.0.1:8787"
	}
	return listen
}

func resolveServeListen(r ConfigReader, channelKey string, channelDefault string) string {
	channelDefault = strings.TrimSpace(channelDefault)
	if r == nil {
		return channelDefault
	}
	if listen := strings.TrimSpace(r.GetString(channelKey)); listen != "" {
		return listen
	}
	if legacy := strings.TrimSpace(r.GetString("server.listen")); legacy != "" {
		return legacy
	}
	return channelDefault
}

func sourceEnabled(sources []string, expected string) bool {
	expected = strings.TrimSpace(strings.ToLower(expected))
	if expected == "" {
		return false
	}
	for _, source := range sources {
		if strings.TrimSpace(strings.ToLower(source)) == expected {
			return true
		}
	}
	return false
}

func normalizeTrimmedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
