package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/cmd/mistermorph/chatcmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/consolecmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/larkcmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/linecmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/runcmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/skillscmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/slackcmd"
	"github.com/quailyquaily/mistermorph/cmd/mistermorph/telegramcmd"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
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
	attachRuntimeFilePreflight(cmd)

	cobra.OnInitialize(initConfig)

	cmd.PersistentFlags().String("config", "", "Config file path (optional).")
	_ = viper.BindPFlag("config", cmd.PersistentFlags().Lookup("config"))

	// Global logging flags (usable across subcommands like run/console/telegram).
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

	registryResolver := newRegistryRuntimeResolver()
	guardResolver := newGuardRuntimeResolver()

	cmd.AddCommand(runcmd.New(runcmd.Dependencies{
		RegistryFromViper:            registryResolver.Registry,
		RegisterTriggeredStaticTools: registryResolver.RegisterTriggeredStaticTools,
		GuardFromViper:               guardResolver.Guard,
	}))
	cmd.AddCommand(chatcmd.New(chatcmd.Dependencies{
		RegistryFromViper:            registryResolver.Registry,
		RegisterTriggeredStaticTools: registryResolver.RegisterTriggeredStaticTools,
		GuardFromViper:               guardResolver.Guard,
	}))

	telegramRuntime := newChannelCommandRuntime()
	cmd.AddCommand(telegramcmd.NewCommand(telegramcmd.Dependencies{
		Dependencies:       telegramRuntime.AwarenessDependencies(registryResolver, guardResolver),
		HandleModelCommand: telegramRuntime.HandleModelCommand,
		HandleSkillCommand: telegramRuntime.HandleSkillCommand,
	}))

	slackRuntime := newChannelCommandRuntime()
	cmd.AddCommand(slackcmd.NewCommand(slackcmd.Dependencies{
		Dependencies:       slackRuntime.AwarenessDependencies(registryResolver, guardResolver),
		HandleModelCommand: slackRuntime.HandleModelCommand,
		HandleSkillCommand: slackRuntime.HandleSkillCommand,
	}))

	lineRuntime := newChannelCommandRuntime()
	cmd.AddCommand(linecmd.NewCommand(linecmd.Dependencies{
		Dependencies:       lineRuntime.AwarenessDependencies(registryResolver, guardResolver),
		HandleModelCommand: lineRuntime.HandleModelCommand,
		HandleSkillCommand: lineRuntime.HandleSkillCommand,
	}))

	larkRuntime := newChannelCommandRuntime()
	cmd.AddCommand(larkcmd.NewCommand(larkcmd.Dependencies{
		Dependencies:       larkRuntime.AwarenessDependencies(registryResolver, guardResolver),
		HandleModelCommand: larkRuntime.HandleModelCommand,
		HandleSkillCommand: larkRuntime.HandleSkillCommand,
	}))
	cmd.AddCommand(newToolsCmd(registryResolver.Registry))
	cmd.AddCommand(newAuthCmd())
	cmd.AddCommand(newBenchmarkCmd())
	cmd.AddCommand(newCreditsCmd())
	cmd.AddCommand(skillscmd.New())
	cmd.AddCommand(consolecmd.New(version))
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
		viper.Set("config", cfgFile)
		expandConfiguredDirKey("file_state_dir")
		expandConfiguredDirKey("file_cache_dir")
	}

}

func resolveConfigFile() (string, bool) {
	explicit := strings.TrimSpace(viper.GetString("config"))
	if explicit != "" {
		return pathutil.ExpandHomePath(explicit), true
	}

	defaultPath := pathutil.DefaultConfigPath()
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, false
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
	return llmutil.BuildRouteClient(
		route,
		nil,
		llmutil.ClientFromConfigWithValues,
		func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
			return llmstats.WrapRuntimeClient(client, cfg.Provider, cfg.Endpoint, cfg.Model, cfg.ContextWindowTokens, slog.Default())
		},
		slog.Default(),
	)
}

func (r *llmRuntimeResolver) CreateImageClient() (llm.ImageClient, error) {
	return llmutil.ImageClientFromValuesWithStats(r.Values(), slog.Default())
}

func (r *llmRuntimeResolver) ResolveRoute(purpose string) (llmutil.ResolvedRoute, error) {
	values := r.Values()
	if strings.TrimSpace(purpose) == llmutil.RoutePurposeMainLoop {
		return llmselect.ResolveMainRoute(values, llmselect.ProcessStore().Get())
	}
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

func (r *skillsRuntimeResolver) Status(currentLoaded []string) (string, error) {
	return skillsutil.RenderSkillStatus(r.Config(), currentLoaded)
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

func (r *registryRuntimeResolver) RegisterTriggeredStaticTools(reg *tools.Registry, triggers map[string]bool) {
	if reg == nil || len(triggers) == 0 {
		return
	}
	registerStaticToolsFromConfig(reg, r.Config(), slog.Default(), nil, triggers)
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

func explicitBuiltinToolsForTask(task string, cfg skillsutil.SkillsConfig) map[string]bool {
	refs := toolsutil.BuiltinToolTriggers(task, skillsutil.ResolveTaskSkillRefs(task, cfg))
	if len(acpclient.AgentsFromViper()) == 0 {
		delete(refs, toolsutil.BuiltinACPSpawn)
	}
	return refs
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
