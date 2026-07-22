package main

import (
	"context"
	"errors"
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
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	envPrefix = "MISTER_MORPH"
)

type rootRuntime struct {
	command          *cobra.Command
	registryResolver *registryRuntimeResolver
}

func (r *rootRuntime) Close() error {
	if r == nil || r.registryResolver == nil {
		return nil
	}
	return r.registryResolver.Close()
}

func ExecuteContext(ctx context.Context) (err error) {
	runtime := newRootRuntime()
	defer func() {
		err = errors.Join(err, runtime.Close())
	}()
	return runtime.command.ExecuteContext(ctx)
}

func newRootRuntime() *rootRuntime {
	cmd := &cobra.Command{
		Use:               "mistermorph",
		Short:             "Unified Agent CLI",
		PersistentPreRunE: runRootPreflight,
	}

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
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := runRootPreflight(cmd, args); err != nil {
			return err
		}
		if shouldPrepareRootRegistry(cmd) {
			if err := guardResolver.Prepare(); err != nil {
				return err
			}
			return registryResolver.Prepare(cmd.Context())
		}
		return nil
	}

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
		Dependencies:       telegramRuntime.Dependencies(registryResolver, guardResolver),
		HandleModelCommand: telegramRuntime.HandleModelCommand,
		HandleSkillCommand: telegramRuntime.HandleSkillCommand,
	}))

	slackRuntime := newChannelCommandRuntime()
	cmd.AddCommand(slackcmd.NewCommand(slackcmd.Dependencies{
		Dependencies:       slackRuntime.Dependencies(registryResolver, guardResolver),
		HandleModelCommand: slackRuntime.HandleModelCommand,
		HandleSkillCommand: slackRuntime.HandleSkillCommand,
	}))

	lineRuntime := newChannelCommandRuntime()
	cmd.AddCommand(linecmd.NewCommand(linecmd.Dependencies{
		Dependencies:       lineRuntime.Dependencies(registryResolver, guardResolver),
		HandleModelCommand: lineRuntime.HandleModelCommand,
		HandleSkillCommand: lineRuntime.HandleSkillCommand,
	}))

	larkRuntime := newChannelCommandRuntime()
	cmd.AddCommand(larkcmd.NewCommand(larkcmd.Dependencies{
		Dependencies:       larkRuntime.Dependencies(registryResolver, guardResolver),
		HandleModelCommand: larkRuntime.HandleModelCommand,
		HandleSkillCommand: larkRuntime.HandleSkillCommand,
	}))
	cmd.AddCommand(newToolsCmd(registryResolver.Registry))
	cmd.AddCommand(newAuthCmd())
	cmd.AddCommand(newBenchmarkCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newCreditsCmd())
	cmd.AddCommand(skillscmd.New())
	cmd.AddCommand(consolecmd.New(version))
	cmd.AddCommand(newInstallCmd())
	cmd.AddCommand(newVersionCmd())

	return &rootRuntime{
		command:          cmd,
		registryResolver: registryResolver,
	}
}

type rootConfigError struct {
	Path string
	Err  error
}

func (e *rootConfigError) Error() string {
	if e == nil {
		return "read config"
	}
	return fmt.Sprintf("read config %q: %v", e.Path, e.Err)
}

func (e *rootConfigError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func loadRootConfig() error {
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
				return nil
			}
			return &rootConfigError{Path: cfgFile, Err: err}
		}
		viper.Set("config", cfgFile)
		expandConfiguredDirKey("file_state_dir")
		expandConfiguredDirKey("file_cache_dir")
	}
	return nil
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
	once      sync.Once
	values    llmutil.RuntimeValues
	paths     runtimepaths.Paths
	valuesErr error
}

func newLLMRuntimeResolver() *llmRuntimeResolver {
	return &llmRuntimeResolver{}
}

func (r *llmRuntimeResolver) Values() (llmutil.RuntimeValues, error) {
	if r == nil {
		return llmutil.RuntimeValues{}, fmt.Errorf("llm runtime resolver is nil")
	}
	r.once.Do(func() {
		reader := viper.GetViper()
		r.values, r.valuesErr = llmutil.RuntimeValuesFromReader(reader)
		r.paths = runtimepaths.FromReader(reader)
	})
	return r.values, r.valuesErr
}

func (r *llmRuntimeResolver) CreateClient(route llmutil.ResolvedRoute) (llm.Client, error) {
	if _, err := r.Values(); err != nil {
		return nil, err
	}
	return llmutil.BuildRouteClient(
		route,
		nil,
		llmutil.ClientFromConfigWithValues,
		func(client llm.Client, cfg llmconfig.ClientConfig, _ string) llm.Client {
			return llmstats.WrapClient(client, llmstats.ClientOptions{
				Provider:            cfg.Provider,
				APIBase:             cfg.Endpoint,
				DefaultModel:        cfg.Model,
				ContextWindowTokens: cfg.ContextWindowTokens,
				JournalDir:          r.paths.LLMUsageJournalDir,
				TopicContextStore:   topiccontext.NewStore(r.paths.TopicContextPath),
				Logger:              slog.Default(),
			})
		},
		slog.Default(),
	)
}

func (r *llmRuntimeResolver) CreateImageClient() (llm.ImageClient, error) {
	values, err := r.Values()
	if err != nil {
		return nil, err
	}
	client, err := llmutil.ImageClientFromValues(values)
	if err != nil {
		return nil, err
	}
	meta := llmutil.ResolveImageClientMetadata(values)
	return llmstats.WrapImageClient(client, llmstats.ClientOptions{
		Provider:     meta.Provider,
		APIBase:      meta.Endpoint,
		DefaultModel: meta.Model,
		JournalDir:   r.paths.LLMUsageJournalDir,
		Logger:       slog.Default(),
	}), nil
}

func (r *llmRuntimeResolver) ResolveRoute(purpose string) (llmutil.ResolvedRoute, error) {
	values, err := r.Values()
	if err != nil {
		return llmutil.ResolvedRoute{}, err
	}
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
	once              sync.Once
	cfg               registryConfig
	cfgErr            error
	registryOnce      sync.Once
	registryErr       error
	baseRegistry      *tools.Registry
	awarenessRegistry *tools.Registry
	mcpHost           *mcphost.Host
	mcpConfigs        func() []mcphost.ServerConfig
	connectMCP        func(context.Context, []mcphost.ServerConfig, *slog.Logger) (*mcphost.Host, error)
	registerMCP       func(*mcphost.Host, *tools.Registry) error
	closeMCP          func(*mcphost.Host) error
	lifecycleMu       sync.Mutex
	closed            bool
	closeOnce         sync.Once
	closeErr          error
}

func newRegistryRuntimeResolver() *registryRuntimeResolver {
	return &registryRuntimeResolver{}
}

func (r *registryRuntimeResolver) Config() registryConfig {
	if r == nil {
		return registryConfig{}
	}
	r.once.Do(func() {
		r.cfg, r.cfgErr = loadRegistryConfigFromViper()
	})
	return r.cfg
}

func (r *registryRuntimeResolver) Registry() *tools.Registry {
	if r == nil {
		return nil
	}
	r.lifecycleMu.Lock()
	registry := r.baseRegistry
	r.lifecycleMu.Unlock()
	if registry == nil {
		slog.Default().Error("tool_registry_not_prepared")
		return nil
	}
	return registry.Clone()
}

func (r *registryRuntimeResolver) AwarenessRegistry() *tools.Registry {
	if r == nil {
		return nil
	}
	r.lifecycleMu.Lock()
	registry := r.awarenessRegistry
	r.lifecycleMu.Unlock()
	if registry == nil {
		slog.Default().Error("awareness_tool_registry_not_prepared")
		return nil
	}
	return registry.Clone()
}

func (r *registryRuntimeResolver) RegisterTriggeredStaticTools(reg *tools.Registry, triggers map[string]bool) {
	if reg == nil || len(triggers) == 0 {
		return
	}
	cfg := r.Config()
	cfg.Common.Awareness = false
	toolsutil.RegisterStaticTools(reg, cfg, nil, triggers)
}

func (r *registryRuntimeResolver) Prepare(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("tool registry resolver is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.registryOnce.Do(func() {
		r.lifecycleMu.Lock()
		defer r.lifecycleMu.Unlock()
		if r.closed {
			r.registryErr = fmt.Errorf("tool registry resolver is closed")
			return
		}
		cfg := r.Config()
		if r.cfgErr != nil {
			r.registryErr = r.cfgErr
			return
		}
		logger := slog.Default()
		baseRegistry := tools.NewRegistry()
		toolsutil.RegisterStaticTools(baseRegistry, cfg, nil, nil)
		awarenessRegistry := tools.NewRegistry()
		cfg.Common.Awareness = true
		toolsutil.RegisterStaticTools(awarenessRegistry, cfg, nil, nil)
		configs := mcphost.MCPConfigFromViper()
		if r.mcpConfigs != nil {
			configs = r.mcpConfigs()
		}
		var host *mcphost.Host
		if len(configs) > 0 {
			var err error
			connect := mcphost.Connect
			if r.connectMCP != nil {
				connect = r.connectMCP
			}
			host, err = connect(ctx, configs, logger)
			if err != nil {
				if host != nil {
					closeHost := func(host *mcphost.Host) error { return host.Close() }
					if r.closeMCP != nil {
						closeHost = r.closeMCP
					}
					err = errors.Join(err, closeHost(host))
				}
				logger.Warn("mcp_init_failed", "stage", "connect", "err", fmt.Errorf("connect MCP servers: %w", err))
				host = nil
			} else if host != nil {
				register := mcphost.RegisterHostTools
				if r.registerMCP != nil {
					register = r.registerMCP
				}
				baseWithMCP := baseRegistry.Clone()
				awarenessWithMCP := awarenessRegistry.Clone()
				if err := register(host, baseWithMCP); err != nil {
					logger.Warn("mcp_init_failed", "stage", "register", "err", fmt.Errorf("register MCP tools: %w", err))
					host = nil
				} else if err := register(host, awarenessWithMCP); err != nil {
					logger.Warn("mcp_init_failed", "stage", "register_awareness", "err", fmt.Errorf("register awareness MCP tools: %w", err))
					host = nil
				} else {
					baseRegistry = baseWithMCP
					awarenessRegistry = awarenessWithMCP
				}
			}
		}
		r.baseRegistry = baseRegistry
		r.awarenessRegistry = awarenessRegistry
		r.mcpHost = host
	})
	return r.registryErr
}

func (r *registryRuntimeResolver) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.lifecycleMu.Lock()
		r.closed = true
		host := r.mcpHost
		r.mcpHost = nil
		r.lifecycleMu.Unlock()
		if host == nil {
			return
		}
		closeHost := func(host *mcphost.Host) error { return host.Close() }
		if r.closeMCP != nil {
			closeHost = r.closeMCP
		}
		r.closeErr = closeHost(host)
	})
	return r.closeErr
}

func shouldPrepareRootRegistry(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	switch cmd.CommandPath() {
	case "mistermorph run", "mistermorph chat", "mistermorph telegram", "mistermorph slack", "mistermorph line", "mistermorph lark", "mistermorph tools":
		return true
	default:
		return false
	}
}

func explicitBuiltinToolsForTask(task string, cfg skillsutil.SkillsConfig) map[string]bool {
	refs := toolsutil.BuiltinToolTriggers(task, skillsutil.ResolveTaskSkillRefs(task, cfg))
	if len(acpclient.AgentsFromViper()) == 0 {
		delete(refs, toolsutil.BuiltinACPSpawn)
	}
	return refs
}

type guardRuntimeResolver struct {
	once   sync.Once
	cfg    guard.Snapshot
	cfgErr error
}

func newGuardRuntimeResolver() *guardRuntimeResolver {
	return &guardRuntimeResolver{}
}

func (r *guardRuntimeResolver) Config() guard.Snapshot {
	if r == nil {
		return guard.Snapshot{}
	}
	r.once.Do(func() {
		r.cfg, r.cfgErr = guard.SnapshotFromReader(viper.GetViper())
	})
	return r.cfg
}

func (r *guardRuntimeResolver) Prepare() error {
	if r == nil {
		return fmt.Errorf("guard runtime resolver is nil")
	}
	_ = r.Config()
	return r.cfgErr
}

func (r *guardRuntimeResolver) Guard(log *slog.Logger) (*guard.Guard, error) {
	return guard.NewChecked(r.Config(), log)
}
