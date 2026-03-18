package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/consolecmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/contactscmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/daemoncmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/larkcmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/linecmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/runcmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/skillscmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/slackcmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/telegramcmd"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	envPrefix = "MISTER_MORPH"
)

func Execute() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mistermorph",
		Short: "Unified Agent CLI",
	}

	cobra.OnInitialize(initConfig)

	cmd.PersistentFlags().String("config", "", "Config file path (optional).")
	_ = viper.BindPFlag("config", cmd.PersistentFlags().Lookup("config"))

	// Global logging flags (usable across subcommands like run/serve/telegram).
	cmd.PersistentFlags().String("log-level", "", "Logging level: debug|info|warn|error (defaults to info).")
	cmd.PersistentFlags().String("log-format", "text", "Logging format: text|json.")
	cmd.PersistentFlags().Bool("log-add-source", false, "Include source file:line in logs.")
	cmd.PersistentFlags().Bool("log-include-thoughts", true, "Include model thoughts in logs (may contain sensitive info).")
	cmd.PersistentFlags().Bool("log-include-tool-params", true, "Include tool params in logs (redacted).")
	cmd.PersistentFlags().Bool("log-include-skill-contents", false, "Include loaded SKILL.md contents in logs (truncated).")
	cmd.PersistentFlags().Int("log-max-thought-chars", 2000, "Max characters of thought to log.")
	cmd.PersistentFlags().Int("log-max-json-bytes", 32768, "Max bytes of JSON params to log.")
	cmd.PersistentFlags().Int("log-max-string-value-chars", 2000, "Max characters per string value in logged params.")
	cmd.PersistentFlags().Int("log-max-skill-content-chars", 8000, "Max characters of SKILL.md content to log.")
	cmd.PersistentFlags().StringArray("log-redact-key", nil, "Extra param keys to redact in logs (repeatable).")

	_ = viper.BindPFlag("logging.level", cmd.PersistentFlags().Lookup("log-level"))
	_ = viper.BindPFlag("logging.format", cmd.PersistentFlags().Lookup("log-format"))
	_ = viper.BindPFlag("logging.add_source", cmd.PersistentFlags().Lookup("log-add-source"))
	_ = viper.BindPFlag("logging.include_thoughts", cmd.PersistentFlags().Lookup("log-include-thoughts"))
	_ = viper.BindPFlag("logging.include_tool_params", cmd.PersistentFlags().Lookup("log-include-tool-params"))
	_ = viper.BindPFlag("logging.include_skill_contents", cmd.PersistentFlags().Lookup("log-include-skill-contents"))
	_ = viper.BindPFlag("logging.max_thought_chars", cmd.PersistentFlags().Lookup("log-max-thought-chars"))
	_ = viper.BindPFlag("logging.max_json_bytes", cmd.PersistentFlags().Lookup("log-max-json-bytes"))
	_ = viper.BindPFlag("logging.max_string_value_chars", cmd.PersistentFlags().Lookup("log-max-string-value-chars"))
	_ = viper.BindPFlag("logging.max_skill_content_chars", cmd.PersistentFlags().Lookup("log-max-skill-content-chars"))
	_ = viper.BindPFlag("logging.redact_keys", cmd.PersistentFlags().Lookup("log-redact-key"))

	viper.SetDefault("logging.format", "text")
	viper.SetDefault("logging.add_source", false)
	viper.SetDefault("logging.include_thoughts", true)
	viper.SetDefault("logging.include_tool_params", true)
	viper.SetDefault("logging.include_skill_contents", false)
	viper.SetDefault("logging.max_thought_chars", 2000)
	viper.SetDefault("logging.max_json_bytes", 32*1024)
	viper.SetDefault("logging.max_string_value_chars", 2000)
	viper.SetDefault("logging.max_skill_content_chars", 8000)

	registryResolver := newRegistryRuntimeResolver()
	guardResolver := newGuardRuntimeResolver()
	telegramLLM := newLLMRuntimeResolver()
	telegramSkills := newSkillsRuntimeResolver()

	cmd.AddCommand(runcmd.New(runcmd.Dependencies{
		RegistryFromViper: registryResolver.Registry,
		GuardFromViper:    guardResolver.Guard,
	}))
	cmd.AddCommand(daemoncmd.NewServeCmd(daemoncmd.ServeDependencies{
		RegistryFromViper: registryResolver.Registry,
		GuardFromViper:    guardResolver.Guard,
	}))
	cmd.AddCommand(daemoncmd.NewSubmitCmd())
	cmd.AddCommand(telegramcmd.NewCommand(telegramcmd.Dependencies{
		Logger:          logutil.LoggerFromViper,
		LogOptions:      logutil.LogOptionsFromViper,
		ResolveLLMRoute: telegramLLM.ResolveRoute,
		CreateLLMClient: telegramLLM.CreateClient,
		Registry:        registryResolver.Registry,
		Guard:           guardResolver.Guard,
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			cfg := telegramSkills.Config()
			if len(stickySkills) > 0 {
				cfg.Requested = append(cfg.Requested, stickySkills...)
			}
			return skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, cfg)
		},
		BuildHeartbeatTask: heartbeatutil.BuildHeartbeatTask,
		BuildHeartbeatMeta: func(source string, interval time.Duration, checklistPath string, checklistEmpty bool, extra map[string]any) map[string]any {
			return heartbeatutil.BuildHeartbeatMeta(source, interval, checklistPath, checklistEmpty, nil, extra)
		},
	}))

	slackLLM := newLLMRuntimeResolver()
	slackSkills := newSkillsRuntimeResolver()
	lineLLM := newLLMRuntimeResolver()
	lineSkills := newSkillsRuntimeResolver()
	larkLLM := newLLMRuntimeResolver()
	larkSkills := newSkillsRuntimeResolver()

	cmd.AddCommand(slackcmd.NewCommand(slackcmd.Dependencies{
		Logger:          logutil.LoggerFromViper,
		LogOptions:      logutil.LogOptionsFromViper,
		ResolveLLMRoute: slackLLM.ResolveRoute,
		CreateLLMClient: slackLLM.CreateClient,
		Registry:        registryResolver.Registry,
		Guard:           guardResolver.Guard,
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			cfg := slackSkills.Config()
			if len(stickySkills) > 0 {
				cfg.Requested = append(cfg.Requested, stickySkills...)
			}
			return skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, cfg)
		},
		BuildHeartbeatTask: heartbeatutil.BuildHeartbeatTask,
		BuildHeartbeatMeta: func(source string, interval time.Duration, checklistPath string, checklistEmpty bool, extra map[string]any) map[string]any {
			return heartbeatutil.BuildHeartbeatMeta(source, interval, checklistPath, checklistEmpty, nil, extra)
		},
	}))
	cmd.AddCommand(linecmd.NewCommand(linecmd.Dependencies{
		Logger:          logutil.LoggerFromViper,
		LogOptions:      logutil.LogOptionsFromViper,
		ResolveLLMRoute: lineLLM.ResolveRoute,
		CreateLLMClient: lineLLM.CreateClient,
		Registry:        registryResolver.Registry,
		Guard:           guardResolver.Guard,
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			cfg := lineSkills.Config()
			if len(stickySkills) > 0 {
				cfg.Requested = append(cfg.Requested, stickySkills...)
			}
			return skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, cfg)
		},
		BuildHeartbeatTask: heartbeatutil.BuildHeartbeatTask,
		BuildHeartbeatMeta: func(source string, interval time.Duration, checklistPath string, checklistEmpty bool, extra map[string]any) map[string]any {
			return heartbeatutil.BuildHeartbeatMeta(source, interval, checklistPath, checklistEmpty, nil, extra)
		},
	}))
	cmd.AddCommand(larkcmd.NewCommand(larkcmd.Dependencies{
		Logger:          logutil.LoggerFromViper,
		LogOptions:      logutil.LogOptionsFromViper,
		ResolveLLMRoute: larkLLM.ResolveRoute,
		CreateLLMClient: larkLLM.CreateClient,
		Registry:        registryResolver.Registry,
		Guard:           guardResolver.Guard,
		PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
			cfg := larkSkills.Config()
			if len(stickySkills) > 0 {
				cfg.Requested = append(cfg.Requested, stickySkills...)
			}
			return skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, cfg)
		},
		BuildHeartbeatTask: heartbeatutil.BuildHeartbeatTask,
		BuildHeartbeatMeta: func(source string, interval time.Duration, checklistPath string, checklistEmpty bool, extra map[string]any) map[string]any {
			return heartbeatutil.BuildHeartbeatMeta(source, interval, checklistPath, checklistEmpty, nil, extra)
		},
	}))
	cmd.AddCommand(newToolsCmd(registryResolver.Registry))
	cmd.AddCommand(skillscmd.New())
	cmd.AddCommand(contactscmd.New())
	cmd.AddCommand(consolecmd.New())
	cmd.AddCommand(newInstallCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}

func initConfig() {
	initViperDefaults()

	viper.SetEnvPrefix(envPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()

	warnf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(os.Stderr, "warn: "+format+"\n", args...)
	}

	cfgFile, explicit := resolveConfigFile()
	if cfgFile != "" {
		if err := configutil.ReadExpandedConfig(viper.GetViper(), cfgFile, warnf); err != nil {
			if !explicit && os.IsNotExist(err) {
				return
			}
			_, _ = fmt.Fprintf(os.Stderr, "Failed to read config: %v\n", err)
			return
		}
		expandConfiguredDirKey("file_state_dir")
		expandConfiguredDirKey("file_cache_dir")
	}

	for _, msg := range deprecatedConfigWarnings(viper.GetViper(), os.LookupEnv) {
		warnf("%s", msg)
	}
}

func deprecatedConfigWarnings(v *viper.Viper, lookupEnv func(string) (string, bool)) []string {
	if v == nil {
		return nil
	}
	listen := strings.TrimSpace(v.GetString("server.listen"))
	if listen == "" {
		return nil
	}

	envSet := false
	if lookupEnv != nil {
		_, envSet = lookupEnv(envPrefix + "_SERVER_LISTEN")
	}
	if !v.InConfig("server.listen") && !envSet {
		return nil
	}

	return []string{
		"server.listen is deprecated; prefer explicit --server-url for submit and channel-specific *.serve_listen for runtime APIs",
	}
}

func resolveConfigFile() (string, bool) {
	explicit := strings.TrimSpace(viper.GetString("config"))
	if explicit != "" {
		return pathutil.ExpandHomePath(explicit), true
	}

	for _, candidate := range []string{"config.yaml", "~/.morph/config.yaml"} {
		resolved := pathutil.ExpandHomePath(candidate)
		if _, err := os.Stat(resolved); err == nil {
			return resolved, false
		}
	}
	return "", false
}

func expandConfiguredDirKey(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	raw := strings.TrimSpace(viper.GetString(key))
	if raw == "" {
		return
	}
	viper.Set(key, pathutil.ExpandHomePath(raw))
}

type llmRuntimeResolver struct {
	once   sync.Once
	values llmutil.RuntimeValues
}

func newLLMRuntimeResolver() *llmRuntimeResolver {
	return &llmRuntimeResolver{}
}

func (r *llmRuntimeResolver) Values() llmutil.RuntimeValues {
	if r == nil {
		return llmutil.RuntimeValues{}
	}
	r.once.Do(func() {
		r.values = llmutil.RuntimeValuesFromViper()
	})
	return r.values
}

func (r *llmRuntimeResolver) CreateClient(route llmutil.ResolvedRoute) (llm.Client, error) {
	base, err := llmutil.ClientFromConfigWithValues(route.ClientConfig, route.Values)
	if err != nil {
		return nil, err
	}
	return llmstats.WrapRuntimeClient(base, route.ClientConfig.Provider, route.ClientConfig.Endpoint, route.ClientConfig.Model, slog.Default()), nil
}

func (r *llmRuntimeResolver) ResolveRoute(purpose string) (llmutil.ResolvedRoute, error) {
	values := r.Values()
	return llmutil.ResolveRoute(values, purpose)
}

type skillsRuntimeResolver struct {
	once sync.Once
	cfg  skillsutil.SkillsConfig
}

func newSkillsRuntimeResolver() *skillsRuntimeResolver {
	return &skillsRuntimeResolver{}
}

func (r *skillsRuntimeResolver) Config() skillsutil.SkillsConfig {
	if r == nil {
		return skillsutil.SkillsConfig{}
	}
	r.once.Do(func() {
		r.cfg = skillsutil.SkillsConfigFromViper()
	})
	cfg := r.cfg
	cfg.Roots = append([]string(nil), cfg.Roots...)
	cfg.Requested = append([]string(nil), cfg.Requested...)
	return cfg
}

type registryRuntimeResolver struct {
	once    sync.Once
	cfg     registryConfig
	mcpOnce sync.Once
	mcpHost *mcphost.Host
}

func newRegistryRuntimeResolver() *registryRuntimeResolver {
	return &registryRuntimeResolver{}
}

func (r *registryRuntimeResolver) Config() registryConfig {
	if r == nil {
		return registryConfig{}
	}
	r.once.Do(func() {
		r.cfg = loadRegistryConfigFromViper()
	})
	return r.cfg
}

func (r *registryRuntimeResolver) Registry() *tools.Registry {
	reg := buildRegistryFromConfig(r.Config(), slog.Default())
	r.ensureMCP()
	if r.mcpHost != nil {
		for _, t := range r.mcpHost.Tools() {
			reg.Register(t)
		}
	}
	return reg
}

func (r *registryRuntimeResolver) ensureMCP() {
	r.mcpOnce.Do(func() {
		logger := slog.Default()
		configs := mcphost.MCPConfigFromViper()
		if len(configs) == 0 {
			return
		}
		host, err := mcphost.Connect(context.Background(), configs, logger)
		if err != nil {
			logger.Warn("mcp_init_failed", "err", err)
			return
		}
		r.mcpHost = host
	})
}

type guardRuntimeResolver struct {
	once sync.Once
	cfg  guardConfigSnapshot
}

func newGuardRuntimeResolver() *guardRuntimeResolver {
	return &guardRuntimeResolver{}
}

func (r *guardRuntimeResolver) Config() guardConfigSnapshot {
	if r == nil {
		return guardConfigSnapshot{}
	}
	r.once.Do(func() {
		r.cfg = loadGuardConfigFromViper()
	})
	return r.cfg
}

func (r *guardRuntimeResolver) Guard(log *slog.Logger) *guard.Guard {
	return buildGuardFromConfig(r.Config(), log)
}
