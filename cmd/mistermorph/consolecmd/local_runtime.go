package consolecmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	heartbeatloop "github.com/quailyquaily/mistermorph/internal/channelruntime/heartbeat"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/heartbeatutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/internal/streaming"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/workspace"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/memory"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

const (
	consoleLocalEndpointRef               = "ep_console_local"
	consoleLocalEndpointName              = "Console Local"
	consoleLocalEndpointURL               = "in-process://console-local"
	consoleTopicTitleMaxChars             = 72
	consoleTopicTitleDirectOutputMaxRunes = 32
	consoleTopicTitleTimeout              = 20 * time.Second
	consoleHeartbeatSkipNoLLM             = "console_submit_unavailable"
)

type consoleLocalTaskJob struct {
	TaskID          string
	ConversationKey string
	TopicID         string
	WorkspaceDir    string
	Task            string
	Model           string
	Timeout         time.Duration
	CreatedAt       time.Time
	Trigger         daemonruntime.TaskTrigger
	AutoRenameTopic bool
	WakeSignal      daemonruntime.PokeInput
	Version         uint64
	Generation      *consoleLocalRuntimeGeneration
}

type consoleLocalRuntimeBundle struct {
	taskRuntime     *taskruntime.Runtime
	mcpHost         *mcphost.Host
	defaultModel    string
	defaultProvider string
}

type consoleLocalRuntimeConfigSnapshot struct {
	reader     *viper.Viper
	commonDeps depsutil.CommonDependencies
}

type consoleLocalRuntimeGeneration struct {
	generation  uint64
	reader      *viper.Viper
	logger      *slog.Logger
	commonDeps  depsutil.CommonDependencies
	bundle      *consoleLocalRuntimeBundle
	memRuntime  runtimecore.MemoryRuntime
	contactsSvc *contacts.Service

	mu      sync.Mutex
	refs    int
	retired bool
	cleaned bool
}

type consoleLocalRuntime struct {
	inspectors            *consoleInspectors
	store                 *daemonruntime.ConsoleFileStore
	bus                   *busruntime.Inproc
	runner                *runtimecore.ConversationRunner[string, consoleLocalTaskJob]
	generationMu          sync.RWMutex
	generation            *consoleLocalRuntimeGeneration
	nextGeneration        uint64
	pendingJobsMu         sync.Mutex
	pendingJobs           map[string]consoleLocalTaskJob
	managedRuntimeMu      sync.RWMutex
	managedRuntimeRunning map[string]bool
	workersCtx            context.Context
	heartbeatMu           sync.Mutex
	streamHub             *consoleStreamHub
	heartbeatState        *heartbeatutil.State
	heartbeatPokeRequests chan heartbeatloop.PokeRequest
	heartbeatCancel       context.CancelFunc
	workspaceStore        *workspace.Store
	handlerMu             sync.RWMutex
	handler               http.Handler
	authToken             string
	cancelWorkers         context.CancelFunc
	seq                   atomic.Uint64
}

type topicDeleterFunc func(id string) bool

func (fn topicDeleterFunc) DeleteTopic(id string) bool {
	if fn == nil {
		return false
	}
	return fn(id)
}

func newConsoleLocalRuntime(cfg serveConfig, reader *viper.Viper) (*consoleLocalRuntime, error) {
	inspectors, err := newConsoleInspectors(cfg.inspectPrompt, cfg.inspectRequest, "console", "console", "20060102_150405")
	if err != nil {
		return nil, err
	}
	out := &consoleLocalRuntime{
		inspectors:            inspectors,
		pendingJobs:           map[string]consoleLocalTaskJob{},
		managedRuntimeRunning: map[string]bool{},
	}
	workersCtx, cancelWorkers := context.WithCancel(context.Background())
	out.workersCtx = workersCtx
	out.cancelWorkers = cancelWorkers
	gen, err := out.prepareGeneration(reader)
	if err != nil {
		_ = inspectors.Close()
		cancelWorkers()
		return nil, err
	}
	slog.SetDefault(gen.logger)
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:          consoleTaskTargetDirFromReader(gen.reader),
		HeartbeatTopicID: strings.TrimSpace(gen.reader.GetString("tasks.targets.console.heartbeat_topic_id")),
		Persist:          consoleTaskPersistenceEnabledFromReader(gen.reader),
	})
	if err != nil {
		_ = inspectors.Close()
		cancelWorkers()
		gen.cleanupNow()
		return nil, err
	}
	maxInFlight := gen.reader.GetInt("bus.max_inflight")
	if maxInFlight <= 0 {
		maxInFlight = 1024
	}
	inprocBus, err := busruntime.StartInproc(busruntime.BootstrapOptions{
		MaxInFlight: maxInFlight,
		Logger:      gen.logger,
		Component:   "console",
	})
	if err != nil {
		_ = inspectors.Close()
		cancelWorkers()
		gen.cleanupNow()
		return nil, err
	}
	out.store = store
	out.bus = inprocBus
	out.streamHub = newConsoleStreamHub()
	out.workspaceStore = workspace.NewStore(consoleWorkspaceAttachmentsPathFromReader(gen.reader))
	out.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workersCtx,
		make(chan struct{}, 1),
		16,
		func(workerCtx context.Context, conversationKey string, job consoleLocalTaskJob) {
			out.handleTaskJob(workerCtx, conversationKey, job)
		},
	)
	if err := inprocBus.Subscribe(busruntime.TopicChatMessage, out.handleConsoleBusMessage); err != nil {
		_ = inspectors.Close()
		inprocBus.Close()
		cancelWorkers()
		gen.cleanupNow()
		return nil, err
	}
	if err := out.applyPreparedGeneration(gen); err != nil {
		_ = inspectors.Close()
		inprocBus.Close()
		cancelWorkers()
		gen.cleanupNow()
		return nil, err
	}
	return out, nil
}

func buildConsoleLocalRuntimeConfigSnapshot(logger *slog.Logger, inspectors *consoleInspectors, reader *viper.Viper) consoleLocalRuntimeConfigSnapshot {
	if logger == nil {
		logger = slog.Default()
	}
	if reader == nil {
		reader = viper.New()
	}
	logOpts := logutil.LogOptionsFromConfig(logutil.LogOptionsConfigFromReader(reader))
	return consoleLocalRuntimeConfigSnapshot{
		reader: reader,
		commonDeps: depsutil.CommonDependencies{
			Logger: func() (*slog.Logger, error) {
				return logger, nil
			},
			LogOptions: func() agent.LogOptions {
				return logOpts
			},
			ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
				values := llmutil.RuntimeValuesFromReader(reader)
				if strings.TrimSpace(purpose) == llmutil.RoutePurposeMainLoop {
					return llmselect.ResolveMainRoute(values, llmselect.ProcessStore().Get())
				}
				return llmutil.ResolveRoute(values, purpose)
			},
			CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
				return llmutil.BuildRouteClient(
					route,
					nil,
					llmutil.ClientFromConfigWithValues,
					func(client llm.Client, cfg llmconfig.ClientConfig, profile string) llm.Client {
						wrappedRoute := route
						wrappedRoute.Profile = strings.TrimSpace(profile)
						wrappedRoute.ClientConfig = cfg
						wrappedRoute.Fallbacks = nil
						wrapped := llmstats.WrapRuntimeClient(client, cfg.Provider, cfg.Endpoint, cfg.Model, logger)
						if inspectors != nil {
							return inspectors.Wrap(wrapped, wrappedRoute)
						}
						return wrapped
					},
					logger,
				)
			},
			RuntimeToolsConfig: toolsutil.LoadRuntimeToolsRegisterConfigFromReader(reader),
			ACPAgents: func() []acpclient.AgentConfig {
				return acpclient.AgentsFromReader(reader)
			},
			PromptSpec: func(ctx context.Context, logger *slog.Logger, logOpts agent.LogOptions, task string, client llm.Client, model string, stickySkills []string) (agent.PromptSpec, []string, error) {
				cfg := skillsutil.SkillsConfigFromReader(reader)
				if len(stickySkills) > 0 {
					cfg.Requested = append(cfg.Requested, stickySkills...)
				}
				return skillsutil.PromptSpecWithSkills(ctx, logger, logOpts, task, client, model, cfg)
			},
		},
	}
}

func consoleDefaultTimeoutFromReader(r interface {
	GetDuration(string) time.Duration
}) time.Duration {
	if r == nil {
		return 10 * time.Minute
	}
	timeout := r.GetDuration("timeout")
	if timeout <= 0 {
		return 10 * time.Minute
	}
	return timeout
}

func consoleLocalRuntimeAuthTokenFromReader(r interface {
	GetString(string) string
}) (string, error) {
	if r != nil {
		if token := strings.TrimSpace(r.GetString("server.auth_token")); token != "" {
			return token, nil
		}
	}
	return consoleLocalRuntimeAuthToken()
}

func consoleLocalRuntimeAuthToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate console local auth token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func consoleMemoryDirFromReader(r interface {
	GetString(string) string
}) string {
	if r == nil {
		return pathutil.ResolveStateChildDir("", "", "memory")
	}
	return pathutil.ResolveStateChildDir(r.GetString("file_state_dir"), r.GetString("memory.dir_name"), "memory")
}

func consoleContactsDirFromReader(r interface {
	GetString(string) string
}) string {
	if r == nil {
		return pathutil.ResolveStateChildDir("", "", "contacts")
	}
	return pathutil.ResolveStateChildDir(r.GetString("file_state_dir"), r.GetString("contacts.dir_name"), "contacts")
}

func consoleTaskTargetDirFromReader(r interface {
	GetString(string) string
}) string {
	if r == nil {
		return pathutil.ResolveStateChildDir("", "tasks/console", "tasks/console")
	}
	tasksDir := pathutil.ResolveStateChildDir(r.GetString("file_state_dir"), r.GetString("tasks.dir_name"), "tasks")
	return pathutil.ResolveStateChildDir(tasksDir, "console", "console")
}

func consoleStateDirFromReader(r interface {
	GetString(string) string
}) string {
	if r == nil {
		return pathutil.ResolveStateDir("")
	}
	return pathutil.ResolveStateDir(r.GetString("file_state_dir"))
}

func consoleWorkspaceAttachmentsPathFromReader(r interface {
	GetString(string) string
}) string {
	return pathutil.ResolveStateFile(consoleStateDirFromReader(r), "workspace_attachments.json")
}

func consoleHeartbeatChecklistPathFromReader(r interface {
	GetString(string) string
}) string {
	return pathutil.ResolveStateFile(consoleStateDirFromReader(r), statepaths.HeartbeatChecklistFilename)
}

func (g *consoleLocalRuntimeGeneration) acquire() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refs++
}

func (g *consoleLocalRuntimeGeneration) release() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.refs > 0 {
		g.refs--
	}
	shouldCleanup := g.retired && g.refs == 0 && !g.cleaned
	if shouldCleanup {
		g.cleaned = true
	}
	g.mu.Unlock()
	if shouldCleanup {
		g.cleanupResources()
	}
}

func (g *consoleLocalRuntimeGeneration) retire() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.retired = true
	shouldCleanup := g.refs == 0 && !g.cleaned
	if shouldCleanup {
		g.cleaned = true
	}
	g.mu.Unlock()
	if shouldCleanup {
		g.cleanupResources()
	}
}

func (g *consoleLocalRuntimeGeneration) cleanupNow() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.cleaned {
		g.mu.Unlock()
		return
	}
	g.cleaned = true
	g.retired = true
	g.mu.Unlock()
	g.cleanupResources()
}

func (g *consoleLocalRuntimeGeneration) cleanupResources() {
	if g == nil {
		return
	}
	if g.bundle != nil && g.bundle.mcpHost != nil {
		_ = g.bundle.mcpHost.Close()
	}
	if g.memRuntime.Cleanup != nil {
		g.memRuntime.Cleanup()
	}
}

func (r *consoleLocalRuntime) prepareGeneration(reader *viper.Viper) (*consoleLocalRuntimeGeneration, error) {
	if r == nil {
		return nil, fmt.Errorf("console runtime is not initialized")
	}
	if reader == nil {
		reader = viper.New()
	}
	logger, err := logutil.LoggerFromConfig(logutil.LoggerConfigFromReader(reader))
	if err != nil {
		return nil, err
	}
	snapshot := buildConsoleLocalRuntimeConfigSnapshot(logger, r.inspectors, reader)
	bundle, err := buildConsoleLocalRuntimeBundle(logger, r.inspectors, snapshot)
	if err != nil {
		return nil, err
	}
	memRuntime, err := runtimecore.NewMemoryRuntime(snapshot.commonDeps, runtimecore.MemoryRuntimeOptions{
		Enabled:       snapshot.reader.GetBool("memory.enabled"),
		ShortTermDays: snapshot.reader.GetInt("memory.short_term_days"),
		MemoryDir:     consoleMemoryDirFromReader(snapshot.reader),
		Logger:        logger,
	})
	if err != nil {
		if bundle.mcpHost != nil {
			_ = bundle.mcpHost.Close()
		}
		return nil, err
	}
	r.generationMu.Lock()
	r.nextGeneration++
	nextGeneration := r.nextGeneration
	r.generationMu.Unlock()
	contactsStore := contacts.NewFileStore(consoleContactsDirFromReader(snapshot.reader))
	generation := &consoleLocalRuntimeGeneration{
		generation: nextGeneration,
		reader:     snapshot.reader,
		logger:     logger,
		commonDeps: snapshot.commonDeps,
		bundle:     bundle,
		memRuntime: memRuntime,
		contactsSvc: contacts.NewServiceWithOptions(contactsStore, contacts.ServiceOptions{
			FailureCooldown: consoleContactsFailureCooldownFromReader(snapshot.reader),
		}),
	}
	return generation, nil
}

func (r *consoleLocalRuntime) currentGeneration() *consoleLocalRuntimeGeneration {
	if r == nil {
		return nil
	}
	r.generationMu.RLock()
	defer r.generationMu.RUnlock()
	return r.generation
}

func (r *consoleLocalRuntime) captureGeneration() (*consoleLocalRuntimeGeneration, error) {
	if r == nil {
		return nil, fmt.Errorf("console runtime is not initialized")
	}
	r.generationMu.RLock()
	generation := r.generation
	if generation != nil {
		generation.acquire()
	}
	r.generationMu.RUnlock()
	if generation == nil {
		return nil, fmt.Errorf("console runtime generation is not initialized")
	}
	return generation, nil
}

func (r *consoleLocalRuntime) currentLogger() *slog.Logger {
	if generation := r.currentGeneration(); generation != nil && generation.logger != nil {
		return generation.logger
	}
	return slog.Default()
}

func (r *consoleLocalRuntime) currentAuthToken() string {
	if r == nil {
		return ""
	}
	r.handlerMu.RLock()
	defer r.handlerMu.RUnlock()
	return strings.TrimSpace(r.authToken)
}

func (r *consoleLocalRuntime) currentHandler() http.Handler {
	if r == nil {
		return nil
	}
	r.handlerMu.RLock()
	defer r.handlerMu.RUnlock()
	return r.handler
}

func (r *consoleLocalRuntime) currentWorkspaceStore() *workspace.Store {
	if r == nil {
		return nil
	}
	r.generationMu.RLock()
	store := r.workspaceStore
	r.generationMu.RUnlock()
	return store
}

func (r *consoleLocalRuntime) currentConfigReader() *viper.Viper {
	generation := r.currentGeneration()
	if generation == nil {
		return viper.New()
	}
	reader := generation.reader
	if reader == nil {
		return viper.New()
	}
	return reader
}

func (r *consoleLocalRuntime) applyPreparedGeneration(generation *consoleLocalRuntimeGeneration) error {
	if r == nil {
		return fmt.Errorf("console runtime is not initialized")
	}
	var reader *viper.Viper
	if generation != nil {
		reader = generation.reader
	}
	authToken, err := consoleLocalRuntimeAuthTokenFromReader(reader)
	if err != nil {
		authToken = ""
	}
	if generation != nil && r.store != nil {
		if err := r.store.ApplyConfig(daemonruntime.ConsoleFileStoreOptions{
			RootDir:          consoleTaskTargetDirFromReader(generation.reader),
			HeartbeatTopicID: strings.TrimSpace(generation.reader.GetString("tasks.targets.console.heartbeat_topic_id")),
			Persist:          consoleTaskPersistenceEnabledFromReader(generation.reader),
		}); err != nil {
			return err
		}
	}
	r.generationMu.Lock()
	prevGeneration := r.generation
	r.generation = generation
	r.workspaceStore = workspace.NewStore(consoleWorkspaceAttachmentsPathFromReader(reader))
	r.generationMu.Unlock()
	r.handlerMu.Lock()
	r.authToken = authToken
	r.handler = daemonruntime.NewHandler(r.routesOptions(strings.TrimSpace(authToken)))
	r.handlerMu.Unlock()
	if generation != nil && generation.memRuntime.ProjectionWorker != nil && r.workersCtx != nil {
		generation.memRuntime.ProjectionWorker.Start(r.workersCtx)
	}
	slog.SetDefault(r.currentLogger())
	r.reloadHeartbeatLoop()
	if prevGeneration != nil {
		prevGeneration.retire()
	}
	return nil
}

func defaultLLMConfigForGeneration(generation *consoleLocalRuntimeGeneration) (string, string) {
	if generation != nil {
		route, err := depsutil.ResolveLLMRouteFromCommon(generation.commonDeps, llmutil.RoutePurposeMainLoop)
		if err == nil {
			return strings.TrimSpace(route.ClientConfig.Provider), strings.TrimSpace(route.ClientConfig.Model)
		}
	}
	var bundle *consoleLocalRuntimeBundle
	if generation != nil {
		bundle = generation.bundle
	}
	if bundle == nil {
		return "", ""
	}
	return strings.TrimSpace(bundle.defaultProvider), strings.TrimSpace(bundle.defaultModel)
}

func buildConsoleLocalRuntimeBundle(
	logger *slog.Logger,
	inspectors *consoleInspectors,
	snapshot consoleLocalRuntimeConfigSnapshot,
) (*consoleLocalRuntimeBundle, error) {
	baseRegistry, mcpHost := buildConsoleBaseRegistryFromReader(context.Background(), logger, snapshot.reader)
	sharedGuard := buildConsoleGuardFromReader(logger, snapshot.reader)
	deps := snapshot.commonDeps
	deps.Registry = func() *tools.Registry { return baseRegistry }
	deps.Guard = func(_ *slog.Logger) *guard.Guard { return sharedGuard }
	rt, err := taskruntime.Bootstrap(deps, taskruntime.BootstrapOptions{
		AgentConfig: consoleAgentConfigFromReader(snapshot.reader),
		EngineToolsConfig: &agent.EngineToolsConfig{
			SpawnEnabled:    consoleEngineToolsConfigFromReader(snapshot.reader).SpawnEnabled,
			ACPSpawnEnabled: consoleEngineToolsConfigFromReader(snapshot.reader).ACPSpawnEnabled,
		},
	})
	if err != nil {
		if mcpHost != nil {
			_ = mcpHost.Close()
		}
		return nil, err
	}
	if warning := consoleLLMCredentialsWarning(rt.BootstrapMainRoute); warning != "" {
		logger.Warn("console_llm_credentials_missing",
			"provider", rt.BootstrapMainProvider,
			"hint", warning,
		)
	}
	return &consoleLocalRuntimeBundle{
		taskRuntime:     rt,
		mcpHost:         mcpHost,
		defaultModel:    rt.BootstrapMainModel,
		defaultProvider: rt.BootstrapMainProvider,
	}, nil
}

func (r *consoleLocalRuntime) ReloadAgentConfigFromReader(reader *viper.Viper) error {
	if r == nil {
		return fmt.Errorf("console runtime is not initialized")
	}
	generation, err := r.prepareGeneration(reader)
	if err != nil {
		return err
	}
	if err := r.applyPreparedGeneration(generation); err != nil {
		generation.cleanupNow()
		return err
	}
	return nil
}

func (r *consoleLocalRuntime) Close() {
	if r == nil {
		return
	}
	r.pendingJobsMu.Lock()
	for taskID, job := range r.pendingJobs {
		if job.Generation != nil {
			job.Generation.release()
		}
		delete(r.pendingJobs, taskID)
	}
	r.pendingJobsMu.Unlock()
	if r.bus != nil {
		_ = r.bus.Close()
	}
	r.heartbeatMu.Lock()
	if r.heartbeatCancel != nil {
		r.heartbeatCancel()
		r.heartbeatCancel = nil
	}
	r.heartbeatPokeRequests = nil
	r.heartbeatMu.Unlock()
	if r.cancelWorkers != nil {
		r.cancelWorkers()
	}
	generation := r.currentGeneration()
	r.generationMu.Lock()
	r.generation = nil
	r.generationMu.Unlock()
	if generation != nil {
		generation.cleanupNow()
	}
	if r.inspectors != nil {
		_ = r.inspectors.Close()
	}
}

func consoleLLMCredentialsWarning(route llmutil.ResolvedRoute) string {
	provider := strings.ToLower(strings.TrimSpace(route.ClientConfig.Provider))
	switch provider {
	case "bedrock":
		// Bedrock may rely on ambient AWS credentials outside llm.* config.
		return ""
	case "cloudflare":
		if strings.TrimSpace(route.Values.CloudflareAccountID) == "" {
			return "set llm.cloudflare.account_id to enable Console Local chat submit"
		}
		if strings.TrimSpace(route.ClientConfig.APIKey) == "" {
			return "set llm.cloudflare.api_token, llm.api_key, or MISTER_MORPH_LLM_API_KEY to enable Console Local chat submit"
		}
		return ""
	default:
		if strings.TrimSpace(route.ClientConfig.APIKey) == "" {
			return "set llm.api_key or MISTER_MORPH_LLM_API_KEY to enable Console Local chat submit"
		}
		return ""
	}
}

func (r *consoleLocalRuntime) Endpoint() runtimeEndpoint {
	return runtimeEndpoint{
		Ref:    consoleLocalEndpointRef,
		Name:   consoleLocalEndpointName,
		URL:    consoleLocalEndpointURL,
		Client: newInProcessRuntimeEndpointClient(r.currentHandler, r.currentAuthToken, r.canSubmit),
	}
}

func (r *consoleLocalRuntime) canSubmit() bool {
	generation, err := r.captureGeneration()
	if err != nil {
		return false
	}
	defer generation.release()
	return canSubmitGeneration(generation)
}

func canSubmitGeneration(generation *consoleLocalRuntimeGeneration) bool {
	if generation == nil {
		return false
	}
	bundle := generation.bundle
	if bundle == nil || bundle.taskRuntime == nil {
		return false
	}
	return consoleLLMCredentialsWarning(bundle.taskRuntime.BootstrapMainRoute) == ""
}

func (r *consoleLocalRuntime) canPokeHeartbeat() bool {
	return r != nil && r.heartbeatPokeRequests != nil
}

func (r *consoleLocalRuntime) pokeHeartbeat(ctx context.Context, input daemonruntime.PokeInput) error {
	if r == nil || r.heartbeatPokeRequests == nil {
		return fmt.Errorf("heartbeat poke is unavailable")
	}
	return heartbeatloop.Trigger(ctx, r.heartbeatPokeRequests, input)
}

func (r *consoleLocalRuntime) workspaceDirForTopic(_ context.Context, topicID string) (string, error) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return "", daemonruntime.BadRequest("topic_id is required")
	}
	store := r.currentWorkspaceStore()
	if store == nil {
		return "", fmt.Errorf("workspace store is not configured")
	}
	return workspace.LookupWorkspaceDir(store, buildConsoleConversationKey(topicID))
}

func (r *consoleLocalRuntime) setWorkspaceDirForTopic(_ context.Context, topicID string, workspaceDir string) (string, error) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return "", daemonruntime.BadRequest("topic_id is required")
	}
	store := r.currentWorkspaceStore()
	if store == nil {
		return "", fmt.Errorf("workspace store is not configured")
	}
	dir, err := workspace.ValidateDir(workspaceDir, nil)
	if err != nil {
		return "", daemonruntime.BadRequest(strings.TrimSpace(err.Error()))
	}
	if _, _, err := store.Set(buildConsoleConversationKey(topicID), workspace.Attachment{WorkspaceDir: dir}); err != nil {
		return "", err
	}
	return dir, nil
}

func (r *consoleLocalRuntime) deleteWorkspaceDirForTopic(_ context.Context, topicID string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return daemonruntime.BadRequest("topic_id is required")
	}
	store := r.currentWorkspaceStore()
	if store == nil {
		return fmt.Errorf("workspace store is not configured")
	}
	_, _, err := store.Delete(buildConsoleConversationKey(topicID))
	return err
}

func daemonruntimeTreeListing(listing workspace.TreeListing) daemonruntime.WorkspaceTreeListing {
	items := make([]daemonruntime.WorkspaceTreeEntry, 0, len(listing.Items))
	for _, item := range listing.Items {
		items = append(items, daemonruntime.WorkspaceTreeEntry{
			Name:        item.Name,
			Path:        item.Path,
			IsDir:       item.IsDir,
			HasChildren: item.HasChildren,
			SizeBytes:   item.SizeBytes,
		})
	}
	return daemonruntime.WorkspaceTreeListing{
		RootPath: listing.RootPath,
		Path:     listing.Path,
		Items:    items,
	}
}

func (r *consoleLocalRuntime) workspaceTreeForTopic(ctx context.Context, topicID string, treePath string) (daemonruntime.WorkspaceTreeListing, error) {
	workspaceDir, err := r.workspaceDirForTopic(ctx, topicID)
	if err != nil {
		return daemonruntime.WorkspaceTreeListing{}, err
	}
	if strings.TrimSpace(workspaceDir) == "" {
		return daemonruntime.WorkspaceTreeListing{}, daemonruntime.BadRequest("workspace is not attached")
	}
	listing, err := workspace.ListAttachedTree(workspaceDir, treePath)
	if err != nil {
		return daemonruntime.WorkspaceTreeListing{}, daemonruntime.BadRequest(strings.TrimSpace(err.Error()))
	}
	return daemonruntimeTreeListing(listing), nil
}

func (r *consoleLocalRuntime) browseWorkspaceTree(_ context.Context, treePath string) (daemonruntime.WorkspaceTreeListing, error) {
	listing, err := workspace.ListSystemTree(treePath)
	if err != nil {
		return daemonruntime.WorkspaceTreeListing{}, daemonruntime.BadRequest(strings.TrimSpace(err.Error()))
	}
	return daemonruntimeTreeListing(listing), nil
}

func (r *consoleLocalRuntime) openWorkspacePathForTopic(ctx context.Context, topicID string, treePath string) error {
	workspaceDir, err := r.workspaceDirForTopic(ctx, topicID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(workspaceDir) == "" {
		return daemonruntime.BadRequest("workspace is not attached")
	}
	targetPath, err := workspace.ResolveAttachedItemPath(workspaceDir, treePath)
	if err != nil {
		return daemonruntime.BadRequest(strings.TrimSpace(err.Error()))
	}
	return workspace.OpenPath(targetPath)
}

func (r *consoleLocalRuntime) deleteTopic(id string) bool {
	if r == nil || r.store == nil {
		return false
	}
	if !r.store.DeleteTopic(id) {
		return false
	}
	store := r.currentWorkspaceStore()
	if store != nil {
		_, _, _ = store.Delete(buildConsoleConversationKey(id))
	}
	return true
}

func (r *consoleLocalRuntime) routesOptions(authToken string) daemonruntime.RoutesOptions {
	var poke daemonruntime.PokeFunc
	if r.canPokeHeartbeat() {
		poke = r.pokeHeartbeat
	}
	return daemonruntime.RoutesOptions{
		Mode: "console",
		AgentNameFunc: func() string {
			generation := r.currentGeneration()
			if generation == nil {
				return personautil.LoadAgentName(consoleStateDirFromReader(viper.New()))
			}
			return personautil.LoadAgentName(consoleStateDirFromReader(generation.reader))
		},
		AuthToken:       strings.TrimSpace(authToken),
		TaskReader:      r.store,
		TopicReader:     r.store,
		TopicDeleter:    topicDeleterFunc(r.deleteTopic),
		Submit:          r.submitTask,
		WorkspaceGet:    r.workspaceDirForTopic,
		WorkspacePut:    r.setWorkspaceDirForTopic,
		WorkspaceDelete: r.deleteWorkspaceDirForTopic,
		WorkspaceOpen:   r.openWorkspacePathForTopic,
		WorkspaceTree:   r.workspaceTreeForTopic,
		WorkspaceBrowse: r.browseWorkspaceTree,
		HealthEnabled:   true,
		Overview: func(ctx context.Context) (map[string]any, error) {
			generation, err := r.captureGeneration()
			if err != nil {
				return nil, err
			}
			defer generation.release()
			provider, model := defaultLLMConfigForGeneration(generation)
			reader := generation.reader
			return map[string]any{
				"llm": map[string]any{
					"provider": provider,
					"model":    model,
				},
				"channel": map[string]any{
					"configured":          true,
					"telegram_configured": strings.TrimSpace(reader.GetString("telegram.bot_token")) != "",
					"slack_configured": strings.TrimSpace(reader.GetString("slack.bot_token")) != "" &&
						strings.TrimSpace(reader.GetString("slack.app_token")) != "",
					"running":          "console",
					"telegram_running": r.isManagedRuntimeRunning("telegram"),
					"slack_running":    r.isManagedRuntimeRunning("slack"),
				},
				"poke_enabled":      r.canPokeHeartbeat(),
				"heartbeat_running": r.heartbeatRunning(),
			}, nil
		},
		Poke: poke,
	}
}

func (r *consoleLocalRuntime) SetManagedRuntimeRunning(kind string, running bool) {
	if r == nil {
		return
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return
	}
	r.managedRuntimeMu.Lock()
	defer r.managedRuntimeMu.Unlock()
	if r.managedRuntimeRunning == nil {
		r.managedRuntimeRunning = map[string]bool{}
	}
	r.managedRuntimeRunning[kind] = running
}

func (r *consoleLocalRuntime) isManagedRuntimeRunning(kind string) bool {
	if r == nil {
		return false
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return false
	}
	r.managedRuntimeMu.RLock()
	defer r.managedRuntimeMu.RUnlock()
	return r.managedRuntimeRunning[kind]
}

func (r *consoleLocalRuntime) submitTask(ctx context.Context, req daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
	generation, err := r.captureGeneration()
	if err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	releaseGeneration := true
	defer func() {
		if releaseGeneration {
			generation.release()
		}
	}()
	timeout := consoleDefaultTimeoutFromReader(generation.reader)
	if strings.TrimSpace(req.Timeout) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(req.Timeout))
		if err != nil || d <= 0 {
			return daemonruntime.SubmitTaskResponse{}, daemonruntime.BadRequest("invalid timeout (use Go duration like 2m, 30s)")
		}
		timeout = d
	}
	trigger := normalizeConsoleTrigger(req.Trigger, daemonruntime.TaskTrigger{
		Source: "ui",
		Event:  "chat_submit",
		Ref:    "web/console",
	})
	task := strings.TrimSpace(req.Task)
	if resp, handled, err := r.handleConsoleWorkspaceCommand(generation, req, timeout, trigger); handled {
		if err == nil {
			releaseGeneration = false
		}
		return resp, err
	}
	if output, handled := r.handleConsoleModelCommand(generation.reader, task); handled {
		resp, err := r.submitSyntheticTask(generation, task, output, timeout, strings.TrimSpace(req.TopicID), strings.TrimSpace(req.TopicTitle), trigger)
		if err == nil {
			releaseGeneration = false
		}
		return resp, err
	}
	model := strings.TrimSpace(req.Model)
	resp, err := r.submitTaskViaBus(
		ctx,
		generation,
		task,
		model,
		timeout,
		strings.TrimSpace(req.TopicID),
		strings.TrimSpace(req.TopicTitle),
		trigger,
	)
	if err == nil {
		releaseGeneration = false
	}
	return resp, err
}

func (r *consoleLocalRuntime) handleConsoleWorkspaceCommand(generation *consoleLocalRuntimeGeneration, req daemonruntime.SubmitTaskRequest, timeout time.Duration, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, bool, error) {
	task := strings.TrimSpace(req.Task)
	cmdWord, cmdArgs := chatcommands.ParseCommand(task)
	if chatcommands.NormalizeCommand(cmdWord) != "/workspace" {
		return daemonruntime.SubmitTaskResponse{}, false, nil
	}
	topicID := strings.TrimSpace(req.TopicID)
	if topicID == "" {
		return daemonruntime.SubmitTaskResponse{}, true, daemonruntime.BadRequest("topic_id is required for /workspace")
	}
	store := r.currentWorkspaceStore()
	if store == nil {
		return daemonruntime.SubmitTaskResponse{}, true, fmt.Errorf("workspace store is not configured")
	}
	result, cmdErr := workspace.ExecuteStoreCommand(store, buildConsoleConversationKey(topicID), cmdArgs, nil)
	output := strings.TrimSpace(result.Reply)
	if cmdErr != nil {
		output = "error: " + strings.TrimSpace(cmdErr.Error())
	}
	resp, err := r.submitSyntheticTask(generation, task, output, timeout, topicID, strings.TrimSpace(req.TopicTitle), trigger)
	return resp, true, err
}

func (r *consoleLocalRuntime) handleConsoleModelCommand(reader *viper.Viper, task string) (string, bool) {
	output, handled, err := llmselect.ExecuteCommandText(
		llmutil.RuntimeValuesFromReader(reader),
		llmselect.ProcessStore(),
		task,
	)
	if !handled {
		return "", false
	}
	if err != nil {
		return "error: " + strings.TrimSpace(err.Error()), true
	}
	return output, true
}

func (r *consoleLocalRuntime) submitSyntheticTask(generation *consoleLocalRuntimeGeneration, task string, output string, timeout time.Duration, topicID string, topicTitle string, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, error) {
	job, _, err := r.acceptTask(generation, task, "", timeout, topicID, topicTitle, trigger)
	if err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	defer generation.release()
	finishedAt := time.Now().UTC()
	r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.FinishedAt = &finishedAt
		info.Result = map[string]any{
			"final": map[string]any{
				"output": strings.TrimSpace(output),
			},
		}
	})
	return daemonruntime.SubmitTaskResponse{
		ID:      job.TaskID,
		Status:  daemonruntime.TaskDone,
		TopicID: job.TopicID,
	}, nil
}

func (r *consoleLocalRuntime) enqueueTask(ctx context.Context, task string, model string, timeout time.Duration, topicID string, topicTitle string, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, error) {
	generation, err := r.captureGeneration()
	if err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	job, resp, err := r.acceptTask(generation, task, model, timeout, topicID, topicTitle, trigger)
	if err != nil {
		generation.release()
		return daemonruntime.SubmitTaskResponse{}, err
	}
	err = r.runner.Enqueue(ctx, job.ConversationKey, func(version uint64) consoleLocalTaskJob {
		job.Version = version
		return job
	})
	if err != nil {
		generation.release()
		runtimecore.MarkTaskFailed(r.store, job.TaskID, strings.TrimSpace(err.Error()), daemonruntime.IsContextDeadline(ctx, err))
		return daemonruntime.SubmitTaskResponse{}, err
	}
	return resp, nil
}

func (r *consoleLocalRuntime) handleTaskJob(workerCtx context.Context, conversationKey string, job consoleLocalTaskJob) {
	if r == nil {
		return
	}
	if job.Generation == nil {
		runtimecore.MarkTaskFailed(r.store, job.TaskID, "console task generation is not initialized", false)
		return
	}
	defer job.Generation.release()
	logger := job.Generation.logger
	if logger == nil {
		logger = r.currentLogger()
	}
	runtimecore.MarkTaskRunning(r.store, job.TaskID)
	if r.streamHub != nil {
		r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskRunning))
	}
	if logger != nil {
		logger.Info("console_stream_enabled",
			"task_id", job.TaskID,
			"conversation_key", conversationKey,
			"topic_id", strings.TrimSpace(job.TopicID),
			"model", strings.TrimSpace(job.Model),
		)
	}

	replySink := newConsoleReplySink(r.streamHub, job.TaskID, logger)
	eventSink := newConsoleEventPreviewSink(r.streamHub, job.TaskID, logger)
	if bundle := job.Generation.bundle; bundle != nil {
		eventSink.observer = newConsoleLLMObserver(bundle.taskRuntime, job.Model, logger)
	}
	streamer := streaming.NewFinalOutputStreamer(streaming.FinalOutputStreamerOptions{
		Sink: replySink,
	})
	streamTracker := newConsoleStreamTracker(logger, job.TaskID)
	onStream := func(event llm.StreamEvent) error {
		return streamTracker.Handle(event, streamer.Handle)
	}

	runCtx, cancel := context.WithTimeout(workerCtx, job.Timeout)
	runCtx = agent.WithEventSinkContext(runCtx, eventSink)
	final, agentCtx, runErr := r.runTask(runCtx, conversationKey, job, onStream)
	contextDeadline := daemonruntime.IsContextDeadline(runCtx, runErr)
	cancel()

	if runErr != nil {
		eventSink.Close()
		displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(runErr))
		if displayErr == "" {
			displayErr = strings.TrimSpace(runErr.Error())
		}
		_ = replySink.Abort(context.Background(), errors.New(displayErr))
		streamTracker.LogSummary("failed")
		r.completeHeartbeatTask(job, heartbeatTaskResultFailure, errors.New(displayErr), time.Time{})
		runtimecore.MarkTaskFailed(r.store, job.TaskID, displayErr, contextDeadline)
		return
	}

	if pendingID, ok := pendingApprovalID(final); ok {
		eventSink.Close()
		if r.streamHub != nil {
			r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskPending))
		}
		pendingAt := time.Now().UTC()
		r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskPending
			info.PendingAt = &pendingAt
			info.ApprovalRequestID = pendingID
			info.Result = buildConsoleTaskResult(final, agentCtx)
		})
		streamTracker.LogSummary("pending")
		r.completeHeartbeatTask(job, heartbeatTaskResultSkipped, nil, time.Time{})
		return
	}

	finishedAt := time.Now().UTC()
	output := strings.TrimSpace(outputfmt.FormatFinalOutput(final))
	eventSink.Close()
	_ = replySink.Finalize(context.Background(), output)
	streamTracker.LogSummary("done")
	r.completeHeartbeatTask(job, heartbeatTaskResultSuccess, nil, finishedAt)
	r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.FinishedAt = &finishedAt
		info.Result = buildConsoleTaskResult(final, agentCtx)
	})
	r.maybeRefreshTopicTitle(job, output)
}

func (r *consoleLocalRuntime) runTask(ctx context.Context, conversationKey string, job consoleLocalTaskJob, onStream llm.StreamHandler) (*agent.Final, *agent.Context, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("console runtime is not initialized")
	}
	if job.Generation == nil {
		return nil, nil, fmt.Errorf("console task generation is not initialized")
	}
	generation := job.Generation
	ctx = llmstats.WithRunID(ctx, job.TaskID)
	ctx = pathroots.WithWorkspaceDir(ctx, job.WorkspaceDir)
	task := strings.TrimSpace(job.Task)
	if task == "" {
		return nil, nil, fmt.Errorf("empty console task")
	}
	model := strings.TrimSpace(job.Model)
	if model == "" {
		_, model = defaultLLMConfigForGeneration(generation)
	}
	historyMsgs, currentMsg, err := r.buildConsolePromptMessages(job)
	if err != nil {
		return nil, nil, err
	}
	memSubjectID := buildConsoleMemorySubjectID(conversationKey)
	memoryHooks := taskruntime.MemoryHooks{
		Source:    "console",
		SubjectID: memSubjectID,
	}
	reader := generation.reader
	if reader.GetBool("memory.enabled") && generation.memRuntime.Orchestrator != nil && memSubjectID != "" {
		memoryHooks.InjectionEnabled = reader.GetBool("memory.injection.enabled")
		memoryHooks.InjectionMaxItems = reader.GetInt("memory.injection.max_items")
		memoryHooks.PrepareInjection = func(maxItems int) (string, error) {
			return generation.memRuntime.Orchestrator.PrepareInjection(memoryruntime.PrepareInjectionRequest{
				SubjectID:      memSubjectID,
				RequestContext: memory.ContextPrivate,
				MaxItems:       maxItems,
			})
		}
		memoryHooks.Record = func(_ *agent.Final, finalOutput string) error {
			_, err := generation.memRuntime.Orchestrator.Record(buildConsoleMemoryRecordRequest(job, memSubjectID, finalOutput))
			return err
		}
		memoryHooks.NotifyRecorded = func() {
			if generation.memRuntime.ProjectionWorker != nil {
				generation.memRuntime.ProjectionWorker.NotifyRecordAppended()
			}
		}
	}
	meta := map[string]any{
		"trigger":          consoleTriggerSource(job.Trigger),
		"console_task_id":  job.TaskID,
		"console_topic_id": strings.TrimSpace(job.TopicID),
	}
	if pokeMeta := job.WakeSignal.MetaValue(); pokeMeta != nil {
		meta["poke"] = pokeMeta
	}
	promptAugment := func(spec *agent.PromptSpec, _ *tools.Registry) {
		if block := workspace.PromptBlock(job.WorkspaceDir); strings.TrimSpace(block.Content) != "" {
			spec.Blocks = append([]agent.PromptBlock{block}, spec.Blocks...)
		}
		if !job.WakeSignal.IsZero() {
			promptprofile.AppendWakeSignalBlock(spec, job.WakeSignal.Normalize())
		}
	}
	bundle := generation.bundle
	if bundle == nil || bundle.taskRuntime == nil {
		return nil, nil, fmt.Errorf("console task runtime is not initialized")
	}
	result, err := bundle.taskRuntime.Run(ctx, taskruntime.RunRequest{
		Task:           task,
		Model:          model,
		Scene:          "console.loop",
		History:        historyMsgs,
		CurrentMessage: currentMsg,
		OnStream:       onStream,
		Meta:           meta,
		PromptAugment:  promptAugment,
		Memory:         memoryHooks,
	})
	if err != nil {
		return result.Final, result.Context, err
	}
	return result.Final, result.Context, nil
}

func buildConsoleTaskResult(final *agent.Final, runCtx *agent.Context) map[string]any {
	out := map[string]any{
		"final": final,
	}
	if runCtx != nil {
		out["metrics"] = buildConsoleTaskMetrics(runCtx.Metrics)
		out["steps"] = summarizeConsoleSteps(runCtx)
	}
	return out
}

// Keep Metrics untagged for resume-state compatibility and normalize only the
// console task projection that is exposed via task logs and APIs.
func buildConsoleTaskMetrics(metrics *agent.Metrics) map[string]any {
	if metrics == nil {
		return nil
	}
	return map[string]any{
		"llm_rounds":    metrics.LLMRounds,
		"total_tokens":  metrics.TotalTokens,
		"total_cost":    metrics.TotalCost,
		"start_time":    metrics.StartTime,
		"elapsed_ms":    metrics.ElapsedMs,
		"tool_calls":    metrics.ToolCalls,
		"parse_retries": metrics.ParseRetries,
	}
}

func (r *consoleLocalRuntime) maybeRefreshTopicTitle(job consoleLocalTaskJob, finalOutput string) {
	if r == nil || !job.AutoRenameTopic {
		return
	}
	topicID := strings.TrimSpace(job.TopicID)
	if topicID == "" || topicID == daemonruntime.ConsoleDefaultTopicID || topicID == r.store.HeartbeatTopicID() {
		return
	}
	taskText := strings.TrimSpace(job.Task)
	if taskText == "" {
		return
	}
	topic, ok := r.store.GetTopic(topicID)
	if !ok || topic == nil {
		return
	}
	if topic.LLMTitleGeneratedAt != nil {
		return
	}
	if title := consoleTopicTitleFromOutput(finalOutput); title != "" {
		if err := r.store.SetTopicTitle(topicID, title); err != nil {
			r.currentLogger().Debug("console_topic_title_update_failed", "topic_id", topicID, "error", err.Error())
		}
		return
	}
	if strings.TrimSpace(finalOutput) == "" {
		return
	}

	if job.Generation != nil {
		job.Generation.acquire()
	}
	go func() {
		if job.Generation != nil {
			defer job.Generation.release()
		}
		if current, ok := r.store.GetTopic(topicID); ok && current != nil && current.LLMTitleGeneratedAt != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), consoleTopicTitleTimeout)
		defer cancel()

		title, err := r.generateTopicTitle(ctx, job.Generation, taskText, finalOutput)
		if err != nil {
			r.currentLogger().Debug("console_topic_title_generate_failed", "topic_id", topicID, "error", err.Error())
			return
		}
		if err := r.store.SetTopicTitleFromLLM(topicID, title); err != nil {
			r.currentLogger().Debug("console_topic_title_update_failed", "topic_id", topicID, "error", err.Error())
		}
	}()
}

func (r *consoleLocalRuntime) generateTopicTitle(ctx context.Context, generation *consoleLocalRuntimeGeneration, task string, finalOutput string) (string, error) {
	if generation == nil {
		return "", fmt.Errorf("console runtime generation is not initialized")
	}
	route, err := depsutil.ResolveLLMRouteFromCommon(generation.commonDeps, llmutil.RoutePurposeMainLoop)
	if err != nil {
		return "", err
	}
	client, err := depsutil.CreateClientFromCommon(generation.commonDeps, route)
	if err != nil {
		return "", err
	}
	model := strings.TrimSpace(route.ClientConfig.Model)
	if model == "" {
		_, model = defaultLLMConfigForGeneration(generation)
	}
	task = daemonruntime.TruncateUTF8(strings.Join(strings.Fields(task), " "), 1200)
	finalOutput = daemonruntime.TruncateUTF8(strings.Join(strings.Fields(finalOutput), " "), 1200)
	if task == "" {
		return "", fmt.Errorf("task is empty")
	}
	if finalOutput == "" {
		finalOutput = "(no final output)"
	}

	result, err := client.Chat(ctx, llm.Request{
		Model: model,
		Scene: "console.topic_title",
		Messages: []llm.Message{
			{
				Role: "system",
				Content: "Generate a short conversation topic title. " +
					"Reply with plain text only, keep the original language, use at most 8 words, and do not wrap the answer in quotes.",
			},
			{
				Role:    "user",
				Content: "User task:\n" + task + "\n\nFinal output:\n" + finalOutput,
			},
		},
	})
	if err != nil {
		return "", err
	}
	title := sanitizeConsoleTopicTitle(result.Text)
	if title == "" {
		return "", fmt.Errorf("generated topic title is empty")
	}
	return title, nil
}

func summarizeConsoleSteps(ctx *agent.Context) []map[string]any {
	if ctx == nil || len(ctx.Steps) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(ctx.Steps))
	for _, s := range ctx.Steps {
		m := map[string]any{
			"step":        s.StepNumber,
			"action":      s.Action,
			"duration_ms": s.Duration.Milliseconds(),
		}
		if s.Error != nil {
			m["error"] = s.Error.Error()
		}
		out = append(out, m)
	}
	return out
}

func consoleTaskPersistenceEnabledFromReader(r interface {
	GetStringSlice(string) []string
}) bool {
	if r == nil {
		return false
	}
	for _, target := range r.GetStringSlice("tasks.persistence_targets") {
		if strings.EqualFold(strings.TrimSpace(target), "console") {
			return true
		}
	}
	return false
}

func buildConsoleConversationKey(topicID string) string {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		topicID = daemonruntime.ConsoleDefaultTopicID
	}
	return "console:" + topicID
}

func seedConsoleTopicTitle(task string, explicit string) string {
	if title := strings.TrimSpace(explicit); title != "" {
		return title
	}
	task = strings.Join(strings.Fields(task), " ")
	if task == "" {
		return ""
	}
	title := daemonruntime.TruncateUTF8(task, consoleTopicTitleMaxChars)
	if len([]rune(task)) > len([]rune(title)) {
		title = strings.TrimSpace(title) + "..."
	}
	return strings.TrimSpace(title)
}

func consoleTopicTitleFromOutput(output string) string {
	output = strings.Join(strings.Fields(strings.TrimSpace(output)), " ")
	if output == "" {
		return ""
	}
	if utf8.RuneCountInString(output) > consoleTopicTitleDirectOutputMaxRunes {
		return ""
	}
	return sanitizeConsoleTopicTitle(output)
}

func sanitizeConsoleTopicTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\"'`")
	raw = strings.Join(strings.Fields(strings.ReplaceAll(raw, "\n", " ")), " ")
	raw = strings.TrimRight(raw, ".,;:!?，。！？；：")
	raw = daemonruntime.TruncateUTF8(raw, consoleTopicTitleMaxChars)
	return strings.TrimSpace(raw)
}

func buildConsoleMemorySubjectID(conversationKey string) string {
	key := strings.TrimSpace(conversationKey)
	if key == "" {
		return buildConsoleConversationKey(daemonruntime.ConsoleDefaultTopicID)
	}
	if strings.HasPrefix(strings.ToLower(key), "console:") {
		return key
	}
	return buildConsoleConversationKey(key)
}

func normalizeConsoleTrigger(in *daemonruntime.TaskTrigger, fallback daemonruntime.TaskTrigger) daemonruntime.TaskTrigger {
	if in == nil {
		return fallback
	}
	trigger := daemonruntime.TaskTrigger{
		Source: strings.TrimSpace(in.Source),
		Event:  strings.TrimSpace(in.Event),
		Ref:    strings.TrimSpace(in.Ref),
	}
	if strings.TrimSpace(trigger.Source) == "" &&
		strings.TrimSpace(trigger.Event) == "" &&
		strings.TrimSpace(trigger.Ref) == "" {
		return fallback
	}
	if strings.TrimSpace(trigger.Source) == "" {
		trigger.Source = fallback.Source
	}
	if strings.TrimSpace(trigger.Event) == "" {
		trigger.Event = fallback.Event
	}
	if strings.TrimSpace(trigger.Ref) == "" {
		trigger.Ref = fallback.Ref
	}
	return trigger
}

func consoleTriggerSource(trigger daemonruntime.TaskTrigger) string {
	if source := strings.TrimSpace(trigger.Source); source != "" {
		return source
	}
	return "console"
}

type heartbeatTaskResult int

const (
	heartbeatTaskResultSuccess heartbeatTaskResult = iota
	heartbeatTaskResultFailure
	heartbeatTaskResultSkipped
)

func isHeartbeatTaskJob(job consoleLocalTaskJob) bool {
	return strings.EqualFold(strings.TrimSpace(job.Trigger.Source), "heartbeat")
}

func (r *consoleLocalRuntime) heartbeatRunning() bool {
	if r == nil || r.heartbeatState == nil {
		return false
	}
	_, _, _, running := r.heartbeatState.Snapshot()
	return running
}

func (r *consoleLocalRuntime) completeHeartbeatTask(
	job consoleLocalTaskJob,
	result heartbeatTaskResult,
	runErr error,
	now time.Time,
) {
	if r == nil || r.heartbeatState == nil || !isHeartbeatTaskJob(job) {
		return
	}
	switch result {
	case heartbeatTaskResultSuccess:
		if now.IsZero() {
			now = time.Now().UTC()
		}
		r.heartbeatState.EndSuccess(now)
	case heartbeatTaskResultSkipped:
		r.heartbeatState.EndSkipped()
	default:
		if runErr == nil {
			runErr = errors.New("heartbeat task failed")
		}
		r.heartbeatState.EndFailure(runErr)
	}
}

func (r *consoleLocalRuntime) enqueueHeartbeatTask(ctx context.Context, task string, _ bool, wakeSignal daemonruntime.PokeInput) string {
	if r == nil {
		return "runtime_unavailable"
	}
	generation, err := r.captureGeneration()
	if err != nil {
		return err.Error()
	}
	releaseGeneration := true
	defer func() {
		if releaseGeneration {
			generation.release()
		}
	}()
	_, model := defaultLLMConfigForGeneration(generation)
	trigger := daemonruntime.TaskTrigger{
		Source: "heartbeat",
		Event:  "heartbeat_tick",
		Ref:    "console",
	}
	if !wakeSignal.IsZero() {
		trigger.Event = "heartbeat_poke"
		trigger.Ref = "console/poke"
	}
	job, _, err := r.acceptTask(
		generation,
		task,
		model,
		consoleDefaultTimeoutFromReader(generation.reader),
		r.store.HeartbeatTopicID(),
		daemonruntime.ConsoleHeartbeatTopicTitle,
		trigger,
	)
	if err != nil {
		return err.Error()
	}
	job.WakeSignal = wakeSignal.Normalize()
	if err := r.runner.Enqueue(ctx, job.ConversationKey, func(version uint64) consoleLocalTaskJob {
		job.Version = version
		return job
	}); err != nil {
		generation.release()
		runtimecore.MarkTaskFailed(r.store, job.TaskID, strings.TrimSpace(err.Error()), daemonruntime.IsContextDeadline(ctx, err))
		return err.Error()
	}
	releaseGeneration = false
	return ""
}

func (r *consoleLocalRuntime) reloadHeartbeatLoop() {
	if r == nil {
		return
	}
	r.heartbeatMu.Lock()
	if r.heartbeatCancel != nil {
		r.heartbeatCancel()
		r.heartbeatCancel = nil
	}
	r.heartbeatPokeRequests = nil
	if r.workersCtx == nil {
		r.heartbeatState = nil
		r.heartbeatMu.Unlock()
		return
	}
	generation := r.currentGeneration()
	if generation == nil {
		r.heartbeatState = nil
		r.heartbeatMu.Unlock()
		return
	}
	hbCfg := channelopts.HeartbeatConfigFromReader(generation.reader)
	if !hbCfg.Enabled || hbCfg.Interval <= 0 {
		r.heartbeatMu.Unlock()
		return
	}
	if r.heartbeatState == nil {
		r.heartbeatState = &heartbeatutil.State{}
	}
	hbState := r.heartbeatState
	hbChecklist := consoleHeartbeatChecklistPathFromReader(generation.reader)
	logger := generation.logger
	hbCtx, cancel := context.WithCancel(r.workersCtx)
	pokeRequests := make(chan heartbeatloop.PokeRequest)
	r.heartbeatCancel = cancel
	r.heartbeatPokeRequests = pokeRequests
	r.heartbeatMu.Unlock()

	runHeartbeatTick := func(wakeSignal daemonruntime.PokeInput) heartbeatutil.TickResult {
		if !r.canSubmit() {
			return heartbeatutil.TickResult{
				Outcome:    heartbeatutil.TickSkipped,
				SkipReason: consoleHeartbeatSkipNoLLM,
			}
		}
		result := heartbeatutil.Tick(
			hbState,
			func() (string, bool, error) {
				return heartbeatutil.BuildHeartbeatTask(hbChecklist)
			},
			func(task string, checklistEmpty bool) string {
				return r.enqueueHeartbeatTask(context.Background(), task, checklistEmpty, wakeSignal)
			},
		)
		switch result.Outcome {
		case heartbeatutil.TickBuildError:
			if strings.TrimSpace(result.AlertMessage) != "" {
				logger.Warn("heartbeat_alert", "source", "console", "message", result.AlertMessage)
			} else if result.BuildError != nil {
				logger.Warn("heartbeat_task_error", "source", "console", "error", result.BuildError.Error())
			}
		case heartbeatutil.TickSkipped:
			if result.SkipReason == consoleHeartbeatSkipNoLLM {
				break
			}
			logger.Debug("heartbeat_skip", "source", "console", "reason", result.SkipReason)
		}
		return result
	}

	go func() {
		heartbeatloop.RunScheduler(hbCtx, heartbeatloop.SchedulerOptions{
			InitialDelay: 15 * time.Second,
			Interval:     hbCfg.Interval,
			PokeRequests: pokeRequests,
		}, runHeartbeatTick)
	}()
}

func buildConsoleMemoryRecordRequest(job consoleLocalTaskJob, subjectID, output string) memoryruntime.RecordRequest {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		subjectID = buildConsoleConversationKey(daemonruntime.ConsoleDefaultTopicID)
	}
	now := time.Now().UTC()
	inbound := newConsoleInboundHistoryItem(job)
	inbound.ChatID = subjectID
	outbound := chathistory.ChatHistoryItem{
		Channel:          consoleHistoryChannel,
		Kind:             chathistory.KindOutboundAgent,
		ChatID:           subjectID,
		ChatType:         "private",
		ReplyToMessageID: job.TaskID,
		SentAt:           now,
		Sender: chathistory.ChatHistorySender{
			UserID:     consoleAgentUserID,
			Username:   consoleAgentUsername,
			Nickname:   consoleAgentNickname,
			IsBot:      true,
			DisplayRef: consoleAgentUsername,
		},
		Text: strings.TrimSpace(output),
	}
	return memoryruntime.RecordRequest{
		TaskRunID:   strings.TrimSpace(job.TaskID),
		SessionID:   subjectID,
		SubjectID:   subjectID,
		Channel:     "console",
		TaskText:    strings.TrimSpace(job.Task),
		FinalOutput: strings.TrimSpace(output),
		Participants: []memory.MemoryParticipant{
			{ID: "console:user", Nickname: "Console User", Protocol: "console"},
			{ID: 0, Nickname: "MisterMorph", Protocol: ""},
		},
		SourceHistory: []chathistory.ChatHistoryItem{
			inbound,
			outbound,
		},
		SessionContext: memory.SessionContext{
			ConversationID:     subjectID,
			ConversationType:   "private",
			CounterpartyID:     "console:user",
			CounterpartyName:   "Console User",
			CounterpartyHandle: "console",
			CounterpartyLabel:  "[Console User](console:user)",
		},
	}
}

func pendingApprovalID(final *agent.Final) (string, bool) {
	if final == nil || final.Output == nil {
		return "", false
	}
	switch v := final.Output.(type) {
	case agent.PendingOutput:
		if strings.EqualFold(strings.TrimSpace(v.Status), "pending") && strings.TrimSpace(v.ApprovalRequestID) != "" {
			return strings.TrimSpace(v.ApprovalRequestID), true
		}
	case *agent.PendingOutput:
		if v != nil && strings.EqualFold(strings.TrimSpace(v.Status), "pending") && strings.TrimSpace(v.ApprovalRequestID) != "" {
			return strings.TrimSpace(v.ApprovalRequestID), true
		}
	case map[string]any:
		st, _ := v["status"].(string)
		id, _ := v["approval_request_id"].(string)
		if strings.EqualFold(strings.TrimSpace(st), "pending") && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id), true
		}
	}
	return "", false
}
