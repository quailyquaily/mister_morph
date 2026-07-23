package consolecmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
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
	awarenessdomain "github.com/quailyquaily/mistermorph/internal/awareness"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	"github.com/quailyquaily/mistermorph/internal/channelopts"
	awarenessloop "github.com/quailyquaily/mistermorph/internal/channelruntime/awareness"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/chatcommands"
	"github.com/quailyquaily/mistermorph/internal/chathistory"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/quailyquaily/mistermorph/internal/contextcheckpoint"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmselect"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/logutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/internal/memoryruntime"
	"github.com/quailyquaily/mistermorph/internal/outputfmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/personautil"
	"github.com/quailyquaily/mistermorph/internal/proaccount"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
	"github.com/quailyquaily/mistermorph/internal/runtimepaths"
	"github.com/quailyquaily/mistermorph/internal/skillsutil"
	"github.com/quailyquaily/mistermorph/internal/streaming"
	"github.com/quailyquaily/mistermorph/internal/taskdomain"
	"github.com/quailyquaily/mistermorph/internal/textutil"
	"github.com/quailyquaily/mistermorph/internal/todo"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/internal/topiccontext"
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
	consoleRuntimeClosedTaskError         = "console runtime closed"
	consoleWorkerPanicTaskError           = "conversation worker panicked"
	consoleApprovalShutdownActor          = "system:console_shutdown"
	consoleApprovalShutdownComment        = "console runtime closed before approval decision"
)

type consoleLocalTaskJob struct {
	TaskID           string
	ConversationKey  string
	TopicID          string
	WorkspaceDir     string
	Task             string
	Model            string
	Route            *llmutil.ResolvedRoute
	LLMProfile       string
	FileReferences   []daemonruntime.FileReference
	Timeout          time.Duration
	CreatedAt        time.Time
	Trigger          daemonruntime.TaskTrigger
	ResumeApprovalID string
	AutoRenameTopic  bool
	WakeSignal       awarenessdomain.PokeInput
	Version          uint64
	Generation       *consoleLocalRuntimeGeneration
}

type consoleLocalRuntimeBundle struct {
	taskRuntime     *taskruntime.Runtime
	mcpHost         *mcphost.Host
	defaultModel    string
	defaultProvider string
}

type consoleLocalRuntimeConfigSnapshot struct {
	reader               *viper.Viper
	llmValues            llmutil.RuntimeValues
	staticRegistryConfig toolsutil.StaticRegistryConfig
	commonDeps           depsutil.CommonDependencies
	paths                runtimepaths.Paths
}

type consoleLocalRuntimeGeneration struct {
	generation  uint64
	reader      *viper.Viper
	llmValues   llmutil.RuntimeValues
	logger      *slog.Logger
	commonDeps  depsutil.CommonDependencies
	bundle      *consoleLocalRuntimeBundle
	memRuntime  runtimecore.MemoryRuntime
	contactsSvc *contacts.Service
	paths       runtimepaths.Paths

	mu      sync.Mutex
	refs    int
	retired bool
	cleaned bool
}

type consoleLocalRuntime struct {
	inspectors                  *consoleInspectors
	store                       *daemonruntime.ConsoleFileStore
	taskUpdater                 daemonruntime.TaskUpdater
	bus                         *busruntime.Inproc
	beforeConsoleInboundPublish func()
	*consoleExecutionState
	lifecycleMu           sync.Mutex
	closed                bool
	generationMu          sync.RWMutex
	generation            *consoleLocalRuntimeGeneration
	nextGeneration        uint64
	managedRuntimeMu      sync.RWMutex
	managedRuntimeRunning map[string]bool
	awarenessMu           sync.Mutex
	streamHub             *consoleStreamHub
	notificationHub       *consoleNotificationHub
	awarenessPokeRequests chan awarenessloop.PokeRequest
	awarenessCronRequests chan awarenessloop.CronRequest
	awarenessCancel       context.CancelFunc
	awarenessDone         chan struct{}
	workspaceStore        *workspace.Store
	runtimePaths          runtimepaths.Paths
	runtimePathsSet       bool
	taskPersistence       bool
	handlerMu             sync.RWMutex
	handler               http.Handler
	authToken             string
	seq                   atomic.Uint64
}

type topicDeleterFunc func(id string) (bool, error)

func (fn topicDeleterFunc) DeleteTopic(id string) (bool, error) {
	if fn == nil {
		return false, nil
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
		managedRuntimeRunning: map[string]bool{},
	}
	out.consoleExecutionState = newConsoleExecutionState(out.expirePendingApproval, out.closePendingApproval)
	out.consoleExecutionState.onDrop = out.handleDroppedTaskJob
	workersCtx := out.workersCtx
	gen, err := out.prepareGeneration(reader)
	if err != nil {
		_ = inspectors.Close()
		out.consoleExecutionState.close()
		return nil, err
	}
	slog.SetDefault(gen.logger)
	persistTasks := consoleTaskPersistenceEnabledFromReader(gen.reader)
	store, err := daemonruntime.NewConsoleFileStore(daemonruntime.ConsoleFileStoreOptions{
		RootDir:    gen.paths.TaskTargetDir("console"),
		Persist:    persistTasks,
		JournalDir: gen.paths.JournalDir,
	})
	if err != nil {
		_ = inspectors.Close()
		out.consoleExecutionState.close()
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
		out.consoleExecutionState.close()
		gen.cleanupNow()
		return nil, err
	}
	out.store = store
	out.runtimePaths = gen.paths
	out.runtimePathsSet = true
	out.taskPersistence = persistTasks
	out.bus = inprocBus
	out.streamHub = newConsoleStreamHub()
	out.notificationHub = newConsoleNotificationHub()
	out.workspaceStore = workspace.NewStore(gen.paths.WorkspaceAttachmentsPath)
	out.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		workersCtx,
		make(chan struct{}, 1),
		16,
		func(workerCtx context.Context, conversationKey string, job consoleLocalTaskJob) {
			out.handleTaskJob(workerCtx, conversationKey, job)
		},
		runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{
			Logger:  gen.logger,
			OnDrop:  out.handleDroppedTaskJob,
			OnPanic: out.handleTaskJobPanic,
		},
	)
	if err := inprocBus.Subscribe(busruntime.TopicChatMessage, out.handleConsoleBusMessage); err != nil {
		_ = inspectors.Close()
		inprocBus.Close()
		out.consoleExecutionState.close()
		gen.cleanupNow()
		return nil, err
	}
	if err := out.applyPreparedGeneration(gen); err != nil {
		_ = inspectors.Close()
		inprocBus.Close()
		out.consoleExecutionState.close()
		gen.cleanupNow()
		return nil, err
	}
	return out, nil
}

func buildConsoleLocalRuntimeConfigSnapshot(logger *slog.Logger, inspectors *consoleInspectors, reader *viper.Viper) (consoleLocalRuntimeConfigSnapshot, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if reader == nil {
		reader = viper.New()
	}
	llmValues, err := llmutil.RuntimeValuesFromReader(reader)
	if err != nil {
		return consoleLocalRuntimeConfigSnapshot{}, err
	}
	staticRegistryConfig, err := toolsutil.StaticRegistryConfigFromReader(reader)
	if err != nil {
		return consoleLocalRuntimeConfigSnapshot{}, err
	}
	logOpts := logutil.LogOptionsFromConfig(logutil.LogOptionsConfigFromReader(reader))
	paths := runtimepaths.FromReader(reader)
	return consoleLocalRuntimeConfigSnapshot{
		reader:               reader,
		llmValues:            llmValues,
		staticRegistryConfig: staticRegistryConfig,
		paths:                paths,
		commonDeps: depsutil.CommonDependencies{
			Logger: func() (*slog.Logger, error) {
				return logger, nil
			},
			LogOptions: func() agent.LogOptions {
				return logOpts
			},
			ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
				if strings.TrimSpace(purpose) == llmutil.RoutePurposeMainLoop {
					return llmselect.ResolveMainRoute(llmValues, llmselect.ProcessStore().Get())
				}
				return llmutil.ResolveRoute(llmValues, purpose)
			},
			ResolveLLMRouteWithProfile: func(purpose, profile string) (llmutil.ResolvedRoute, error) {
				return llmutil.ResolveRouteWithProfileOverride(llmValues, purpose, profile)
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
						wrapped := llmstats.WrapClient(client, llmstats.ClientOptions{
							Provider:            cfg.Provider,
							APIBase:             cfg.Endpoint,
							DefaultModel:        cfg.Model,
							ContextWindowTokens: cfg.ContextWindowTokens,
							JournalDir:          paths.LLMUsageJournalDir,
							TopicContextStore:   topiccontext.NewStore(paths.TopicContextPath),
							Logger:              logger,
						})
						if inspectors != nil {
							return inspectors.Wrap(wrapped, wrappedRoute)
						}
						return wrapped
					},
					logger,
				)
			},
			CreateImageClient: func() (llm.ImageClient, error) {
				client, err := llmutil.ImageClientFromValues(llmValues)
				if err != nil {
					return nil, err
				}
				meta := llmutil.ResolveImageClientMetadata(llmValues)
				return llmstats.WrapImageClient(client, llmstats.ClientOptions{
					Provider:     meta.Provider,
					APIBase:      meta.Endpoint,
					DefaultModel: meta.Model,
					JournalDir:   paths.LLMUsageJournalDir,
					Logger:       logger,
				}), nil
			},
			RuntimeToolsConfig: toolsutil.LoadRuntimeToolsRegisterConfigFromReader(reader),
			RuntimePaths:       paths,
			ToolTriggers: func(task string) map[string]bool {
				cfg := skillsutil.SkillsConfigFromReader(reader)
				refs := toolsutil.BuiltinToolTriggers(task, skillsutil.ResolveTaskSkillRefs(task, cfg))
				if len(acpclient.AgentsFromReader(reader)) == 0 {
					delete(refs, toolsutil.BuiltinACPSpawn)
				}
				return refs
			},
			RegisterTriggeredStaticTools: func(reg *tools.Registry, triggers map[string]bool) {
				toolsutil.RegisterStaticTools(reg, staticRegistryConfig, nil, triggers)
			},
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
	}, nil
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

func consoleAgentLimitsFromReader(r interface {
	GetInt(string) int
	GetBool(string) bool
	GetFloat64(string) float64
}) agent.Limits {
	if r == nil {
		return agent.Limits{}
	}
	return agent.Limits{
		MaxSteps:        r.GetInt("max_steps"),
		ParseRetries:    r.GetInt("parse_retries"),
		MaxTokenBudget:  r.GetInt("max_token_budget"),
		ToolRepeatLimit: r.GetInt("tool_repeat_limit"),
		ContextCompaction: agent.NewContextCompactionConfig(
			r.GetBool("context_compaction.enabled"),
			r.GetFloat64("context_compaction.trigger_ratio"),
			r.GetInt("context_compaction.output_reserve_tokens"),
		),
	}
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

func (g *consoleLocalRuntimeGeneration) acquire() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refs++
}

func (g *consoleLocalRuntimeGeneration) tryAcquire() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.retired || g.cleaned {
		return false
	}
	g.refs++
	return true
}

func consoleGenerationHandler(generation *consoleLocalRuntimeGeneration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if next == nil || !generation.tryAcquire() {
			http.Error(w, "console runtime generation is unavailable", http.StatusServiceUnavailable)
			return
		}
		defer generation.release()
		next.ServeHTTP(w, req)
	})
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
	if g.markRetired() {
		g.cleanupResources()
	}
}

// markRetired closes admission before cleanup starts. Callers that also own the
// runtime generation pointer can use it while holding generationMu, making the
// pointer removal and late-handler rejection one atomic state transition.
func (g *consoleLocalRuntimeGeneration) markRetired() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	g.retired = true
	shouldCleanup := g.refs == 0 && !g.cleaned
	if shouldCleanup {
		g.cleaned = true
	}
	g.mu.Unlock()
	return shouldCleanup
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
	if g.memRuntime.Cleanup != nil {
		g.memRuntime.Cleanup()
	}
	if g.bundle != nil && g.bundle.taskRuntime != nil {
		if err := g.bundle.taskRuntime.Close(); err != nil && g.logger != nil {
			g.logger.Warn("console_task_runtime_close_failed", "error", err.Error())
		}
	}
	if g.bundle != nil && g.bundle.mcpHost != nil {
		_ = g.bundle.mcpHost.Close()
	}
}

func (r *consoleLocalRuntime) prepareGeneration(reader *viper.Viper) (*consoleLocalRuntimeGeneration, error) {
	if r == nil {
		return nil, fmt.Errorf("console runtime is not initialized")
	}
	if reader == nil {
		reader = viper.New()
	}
	paths := runtimepaths.FromReader(reader)
	persistTasks := consoleTaskPersistenceEnabledFromReader(reader)
	if r.runtimePathsSet && (paths != r.runtimePaths || persistTasks != r.taskPersistence) {
		return nil, fmt.Errorf("console runtime persistence paths are boot-only; restart to change file_state_dir, file_cache_dir, or derived state paths")
	}
	logger, err := logutil.LoggerFromConfig(logutil.LoggerConfigFromReader(reader))
	if err != nil {
		return nil, err
	}
	snapshot, err := buildConsoleLocalRuntimeConfigSnapshot(logger, r.inspectors, reader)
	if err != nil {
		return nil, err
	}
	bundle, commonDeps, err := buildConsoleLocalRuntimeBundle(logger, r.inspectors, snapshot)
	if err != nil {
		return nil, err
	}
	memRuntime, err := runtimecore.NewMemoryRuntime(commonDeps, runtimecore.MemoryRuntimeOptions{
		Enabled:       snapshot.reader.GetBool("memory.enabled"),
		ShortTermDays: snapshot.reader.GetInt("memory.short_term_days"),
		MemoryDir:     snapshot.paths.MemoryDir,
		JournalDir:    snapshot.paths.JournalDir,
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
	contactsStore := contacts.NewFileStore(snapshot.paths.ContactsDir)
	generation := &consoleLocalRuntimeGeneration{
		generation: nextGeneration,
		reader:     snapshot.reader,
		llmValues:  snapshot.llmValues,
		logger:     logger,
		commonDeps: commonDeps,
		bundle:     bundle,
		memRuntime: memRuntime,
		contactsSvc: contacts.NewServiceWithOptions(contactsStore, contacts.ServiceOptions{
			FailureCooldown: consoleContactsFailureCooldownFromReader(snapshot.reader),
		}),
		paths: snapshot.paths,
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

func (r *consoleLocalRuntime) currentEndpointView() inProcessRuntimeView {
	if r == nil {
		return inProcessRuntimeView{}
	}
	r.handlerMu.RLock()
	defer r.handlerMu.RUnlock()
	return inProcessRuntimeView{
		handler:   r.handler,
		authToken: strings.TrimSpace(r.authToken),
	}
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
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.applyPreparedGenerationLocked(generation)
}

func (r *consoleLocalRuntime) applyPreparedGenerationLocked(generation *consoleLocalRuntimeGeneration) error {
	if r.closed {
		return errConsoleExecutionClosed
	}
	var reader *viper.Viper
	if generation != nil {
		reader = generation.reader
	}
	authToken, err := consoleLocalRuntimeAuthTokenFromReader(reader)
	if err != nil {
		authToken = ""
	}
	if generation != nil && r.runtimePathsSet && (generation.paths != r.runtimePaths || consoleTaskPersistenceEnabledFromReader(generation.reader) != r.taskPersistence) {
		return fmt.Errorf("console runtime persistence paths are boot-only; restart to change file_state_dir, file_cache_dir, or derived state paths")
	}
	r.generationMu.Lock()
	prevGeneration := r.generation
	r.generation = generation
	if r.workspaceStore == nil && generation != nil {
		r.workspaceStore = workspace.NewStore(generation.paths.WorkspaceAttachmentsPath)
	}
	r.generationMu.Unlock()
	r.handlerMu.Lock()
	r.authToken = authToken
	r.handler = consoleGenerationHandler(generation, daemonruntime.NewHandler(r.routesOptions(strings.TrimSpace(authToken))))
	r.handlerMu.Unlock()
	if generation != nil && generation.memRuntime.ProjectionWorker != nil && r.workersCtx != nil {
		generation.memRuntime.ProjectionWorker.Start(r.workersCtx)
	}
	slog.SetDefault(r.currentLogger())
	r.reloadAwarenessLoop()
	if prevGeneration != nil {
		prevGeneration.retire()
	}
	return nil
}

func defaultLLMConfigForGeneration(generation *consoleLocalRuntimeGeneration) (string, string) {
	if generation != nil && generation.commonDeps.ResolveLLMRoute != nil {
		route, err := generation.commonDeps.ResolveLLMRoute(llmutil.RoutePurposeMainLoop)
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

func resolveConsoleAdmittedRoute(generation *consoleLocalRuntimeGeneration, task string, requestedModel string, requestedProfile string, selectionKey string) (llmutil.ResolvedRoute, string, error) {
	requestedModel = strings.TrimSpace(requestedModel)
	requestedProfile = strings.TrimSpace(requestedProfile)
	_, thinkTask := chatcommands.ExtractThinkTask(task)
	if generation == nil {
		return llmutil.ResolvedRoute{}, "", fmt.Errorf("console runtime generation is not initialized")
	}
	purpose := llmutil.RoutePurposeMainLoop
	if thinkTask {
		purpose = llmutil.RoutePurposeThink
	}
	var (
		route llmutil.ResolvedRoute
		err   error
	)
	if requestedProfile != "" {
		if generation.commonDeps.ResolveLLMRouteWithProfile == nil {
			return llmutil.ResolvedRoute{}, "", fmt.Errorf("llm profile selection is unavailable")
		}
		route, err = generation.commonDeps.ResolveLLMRouteWithProfile(purpose, requestedProfile)
	} else if generation.commonDeps.ResolveLLMRoute != nil {
		route, err = generation.commonDeps.ResolveLLMRoute(purpose)
	} else {
		provider, model := defaultLLMConfigForGeneration(generation)
		route = llmutil.ResolvedRoute{
			Purpose: purpose,
			ClientConfig: llmconfig.ClientConfig{
				Provider: provider,
				Model:    model,
			},
		}
	}
	if err != nil {
		return llmutil.ResolvedRoute{}, "", err
	}
	route = llmutil.SelectRouteCandidate(route, selectionKey)
	if thinkTask {
		route = llmutil.ResolvedRouteWithReasoningEffort(route, llmutil.ReasoningEffortXHigh)
	}
	model := strings.TrimSpace(route.ClientConfig.Model)
	if !thinkTask && requestedProfile == "" && requestedModel != "" {
		model = requestedModel
	}
	return route, model, nil
}

func buildConsoleLocalRuntimeBundle(
	logger *slog.Logger,
	inspectors *consoleInspectors,
	snapshot consoleLocalRuntimeConfigSnapshot,
) (*consoleLocalRuntimeBundle, depsutil.CommonDependencies, error) {
	baseRegistry, awarenessRegistry, mcpHost, err := buildConsoleRegistriesFromReader(context.Background(), logger, snapshot.reader)
	if err != nil {
		return nil, depsutil.CommonDependencies{}, err
	}
	guardSnapshot, err := guard.SnapshotFromReader(snapshot.reader)
	if err != nil {
		if mcpHost != nil {
			_ = mcpHost.Close()
		}
		return nil, depsutil.CommonDependencies{}, err
	}
	deps := snapshot.commonDeps
	deps.Registry = func() *tools.Registry { return baseRegistry }
	deps.AwarenessRegistry = func() *tools.Registry { return awarenessRegistry }
	deps.Guard = func(guardLogger *slog.Logger) (*guard.Guard, error) {
		if guardLogger == nil {
			guardLogger = logger
		}
		return guard.NewChecked(guardSnapshot, guardLogger)
	}
	engineToolsConfig := consoleEngineToolsConfigFromReader(snapshot.reader)
	rt, err := taskruntime.Bootstrap(deps, taskruntime.BootstrapOptions{
		AgentConfig:       consoleAgentLimitsFromReader(snapshot.reader).ToConfig(),
		EngineToolsConfig: &engineToolsConfig,
	})
	if err != nil {
		if mcpHost != nil {
			_ = mcpHost.Close()
		}
		return nil, depsutil.CommonDependencies{}, err
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
	}, deps, nil
}

func (r *consoleLocalRuntime) ReloadAgentConfigFromReader(reader *viper.Viper) error {
	if r == nil {
		return fmt.Errorf("console runtime is not initialized")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.closed {
		return errConsoleExecutionClosed
	}
	generation, err := r.prepareGeneration(reader)
	if err != nil {
		return err
	}
	if err := r.applyPreparedGenerationLocked(generation); err != nil {
		generation.cleanupNow()
		return err
	}
	return nil
}

func (r *consoleLocalRuntime) Close() {
	if r == nil {
		return
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.generationMu.Lock()
	generation := r.generation
	r.generation = nil
	shouldCleanupGeneration := generation.markRetired()
	r.generationMu.Unlock()
	if shouldCleanupGeneration {
		generation.cleanupResources()
	}
	if r.consoleExecutionState != nil {
		r.consoleExecutionState.close()
	}
	r.stopAwarenessLoop()
	if r.bus != nil {
		_ = r.bus.Close()
	}
	if r.inspectors != nil {
		_ = r.inspectors.Close()
	}
}

func consoleLLMCredentialsWarning(route llmutil.ResolvedRoute) string {
	if strings.EqualFold(strings.TrimSpace(route.Values.InferenceProvider), llmutil.InferenceProviderMisterMorphPro) {
		status := proaccount.ReadStatus(route.Values.FileStateDir, time.Now().UTC())
		if !status.LoggedIn || !status.SubscriptionAPIKeyPresent {
			return "sign in with MisterMorph Pro to enable Console Local chat submit"
		}
		if !status.FileModeOK {
			return "fix MisterMorph Pro session file permissions to enable Console Local chat submit"
		}
		return ""
	}
	provider := strings.ToLower(strings.TrimSpace(route.ClientConfig.Provider))
	switch provider {
	case "bedrock":
		// Bedrock may rely on ambient AWS credentials outside llm.* config.
		return ""
	case "openai_codex":
		status := codexauth.ReadStatus(route.Values.FileStateDir, time.Now().UTC())
		if !status.LoggedIn {
			return "sign in with OpenAI Codex OAuth to enable Console Local chat submit"
		}
		if !status.FileModeOK {
			return "fix OpenAI Codex token file permissions to enable Console Local chat submit"
		}
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
		Client: newInProcessRuntimeEndpointClient(r.currentEndpointView, r.canSubmit),
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

func (r *consoleLocalRuntime) canPokeAwareness() bool {
	if r == nil {
		return false
	}
	r.awarenessMu.Lock()
	defer r.awarenessMu.Unlock()
	return r.awarenessPokeRequests != nil
}

func (r *consoleLocalRuntime) pokeAwareness(ctx context.Context, input awarenessdomain.PokeInput) error {
	if r == nil {
		return fmt.Errorf("awareness poke is unavailable")
	}
	r.awarenessMu.Lock()
	pokeRequests := r.awarenessPokeRequests
	r.awarenessMu.Unlock()
	if pokeRequests == nil {
		return fmt.Errorf("awareness poke is unavailable")
	}
	return awarenessloop.Trigger(ctx, pokeRequests, input)
}

func (r *consoleLocalRuntime) canRunCron() bool {
	if r == nil {
		return false
	}
	r.awarenessMu.Lock()
	defer r.awarenessMu.Unlock()
	return r.awarenessCronRequests != nil
}

func (r *consoleLocalRuntime) runCron(ctx context.Context, task cronstore.Task) error {
	if r == nil {
		return fmt.Errorf("cron trigger is unavailable")
	}
	r.awarenessMu.Lock()
	cronRequests := r.awarenessCronRequests
	r.awarenessMu.Unlock()
	if cronRequests == nil {
		return fmt.Errorf("cron trigger is unavailable")
	}
	return awarenessloop.TriggerCron(ctx, cronRequests, task)
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

func (r *consoleLocalRuntime) topicMetadataForTopic(ctx context.Context, topicID, topicContextPath string) (daemonruntime.TopicMetadata, error) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return daemonruntime.TopicMetadata{}, daemonruntime.BadRequest("topic_id is required")
	}
	conversationKey := buildConsoleConversationKey(topicID)
	workspaceDir, err := r.workspaceDirForTopic(ctx, topicID)
	if err != nil {
		return daemonruntime.TopicMetadata{}, err
	}
	payload := daemonruntime.TopicMetadata{
		TopicID:         topicID,
		ConversationKey: conversationKey,
		Workspace: daemonruntime.TopicMetadataWorkspace{
			WorkspaceDir: workspaceDir,
		},
	}
	item, ok, err := topiccontext.NewStore(topicContextPath).Get(conversationKey)
	if err != nil {
		return daemonruntime.TopicMetadata{}, err
	}
	if ok {
		payload.Context = daemonruntime.TopicMetadataContext{
			Available:                true,
			Model:                    item.Model,
			NormalizedModel:          item.NormalizedModel,
			ContextWindowTokens:      item.ContextWindowTokens,
			ContextWindowSource:      item.ContextWindowSource,
			UsedInputTokens:          item.UsedInputTokens,
			CachedInputTokens:        item.CachedInputTokens,
			CacheCreationInputTokens: item.CacheCreationInputTokens,
			UsageRatio:               item.UsageRatio,
			LastRunID:                item.LastRunID,
			LastOriginEventID:        item.LastOriginEventID,
			UpdatedAt:                item.UpdatedAt,
		}
	}
	return payload, nil
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

func (r *consoleLocalRuntime) browseWorkspaceTree(_ context.Context, treePath string, showHidden bool) (daemonruntime.WorkspaceTreeListing, error) {
	listing, err := workspace.ListSystemTree(treePath, showHidden)
	if err != nil {
		return daemonruntime.WorkspaceTreeListing{}, daemonruntime.BadRequest(strings.TrimSpace(err.Error()))
	}
	return daemonruntimeTreeListing(listing), nil
}

func (r *consoleLocalRuntime) createWorkspaceDir(_ context.Context, parentPath string, name string) (string, error) {
	createdPath, err := workspace.CreateSystemDir(parentPath, name)
	if err != nil {
		return "", daemonruntime.BadRequest(strings.TrimSpace(err.Error()))
	}
	return createdPath, nil
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

func (r *consoleLocalRuntime) deleteTopic(id string) (bool, error) {
	if r == nil || r.store == nil {
		return false, fmt.Errorf("console task store is unavailable")
	}
	deleted, err := r.store.DeleteTopic(id)
	if err != nil || !deleted {
		return deleted, err
	}
	conversationKey := buildConsoleConversationKey(id)
	if r.runControl != nil {
		r.runControl.Stop("console", conversationKey, "topic_deleted")
	}
	checkpointRoot := r.runtimePaths.CheckpointRoot
	logger := slog.Default()
	if generation := r.currentGeneration(); generation != nil {
		checkpointRoot = generation.paths.CheckpointRoot
		if generation.logger != nil {
			logger = generation.logger
		}
	}
	if err := contextcheckpoint.Reset(context.Background(), checkpointRoot, conversationKey); err != nil {
		logger.Warn("console_context_checkpoint_reset_failed", "topic_id", id, "error", err.Error())
	}
	store := r.currentWorkspaceStore()
	if store != nil {
		_, _, _ = store.Delete(conversationKey)
	}
	return true, nil
}

func (r *consoleLocalRuntime) routesOptions(authToken string) daemonruntime.RoutesOptions {
	generation := r.currentGeneration()
	paths := r.runtimePaths
	reader := viper.New()
	if generation != nil {
		paths = generation.paths
		if generation.reader != nil {
			reader = generation.reader
		}
	}
	return daemonruntime.RoutesOptions{
		Mode: "console",
		AgentNameFunc: func() string {
			return personautil.LoadAgentName(paths.StateDir)
		},
		AuthToken: strings.TrimSpace(authToken),
		TaskTopic: daemonruntime.TaskTopicRoutes{
			TaskReader:   r.store,
			TopicReader:  r.store,
			TopicDeleter: topicDeleterFunc(r.deleteTopic),
			Submit: func(ctx context.Context, req daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
				if generation == nil {
					return daemonruntime.SubmitTaskResponse{}, fmt.Errorf("console runtime generation is not initialized")
				}
				generation.acquire()
				return r.submitTaskWithGeneration(ctx, generation, req)
			},
			Stop: r.stopTask,
			TopicMetadata: func(ctx context.Context, topicID string) (daemonruntime.TopicMetadata, error) {
				return r.topicMetadataForTopic(ctx, topicID, paths.TopicContextPath)
			},
		},
		Approvals: daemonruntime.ApprovalRoutes{
			List:    r.listApprovals,
			Approve: r.approveApproval,
			Deny:    r.denyApproval,
		},
		Workspace: daemonruntime.WorkspaceRoutes{
			Get:       r.workspaceDirForTopic,
			Put:       r.setWorkspaceDirForTopic,
			Delete:    r.deleteWorkspaceDirForTopic,
			Open:      r.openWorkspacePathForTopic,
			Tree:      r.workspaceTreeForTopic,
			Browse:    r.browseWorkspaceTree,
			CreateDir: r.createWorkspaceDir,
		},
		HealthEnabled:       true,
		RuntimePaths:        paths,
		AgentSettingsReader: reader,
		Overview: func(ctx context.Context) (map[string]any, error) {
			if generation == nil {
				return nil, fmt.Errorf("console runtime generation is not initialized")
			}
			generation.acquire()
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
					"lark_configured": strings.TrimSpace(reader.GetString("lark.app_id")) != "" &&
						strings.TrimSpace(reader.GetString("lark.app_secret")) != "",
					"running":          "console",
					"telegram_running": r.isManagedRuntimeRunning("telegram"),
					"slack_running":    r.isManagedRuntimeRunning("slack"),
					"lark_running":     r.isManagedRuntimeRunning("lark"),
				},
				"poke_enabled":     r.canPokeAwareness(),
				"cron_run_enabled": r.canRunCron(),
			}, nil
		},
		Poke:    r.pokeAwareness,
		CronRun: r.runCron,
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
	return r.submitTaskWithGeneration(ctx, generation, req)
}

func (r *consoleLocalRuntime) submitTaskWithGeneration(ctx context.Context, generation *consoleLocalRuntimeGeneration, req daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
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
	contextCompactionOnly := chatcommands.IsContextCompactCommand(task)
	if len(req.FileReferences) == 0 {
		if resp, handled, err := r.handleConsoleRuntimeCommand(generation, req, timeout, trigger); handled {
			return resp, err
		}
	}
	if !contextCompactionOnly && len(req.FileReferences) == 0 {
		if result := r.trySteerConsoleRun(task, strings.TrimSpace(req.TopicID)); result.Found {
			output := runtimecontrol.SteerFeedback(result.Found, result.Queued)
			steerTargetTaskID := ""
			if result.Queued {
				steerTargetTaskID = strings.TrimSpace(result.TaskID)
			}
			resp, err := r.submitSyntheticTask(
				generation,
				task,
				output,
				steerTargetTaskID,
				timeout,
				strings.TrimSpace(req.TopicID),
				strings.TrimSpace(req.TopicTitle),
				strings.TrimSpace(req.WorkspaceDir),
				trigger,
			)
			return resp, err
		}
	}
	resp, ownershipTransferred, err := r.submitTaskViaBus(
		ctx,
		generation,
		task,
		strings.TrimSpace(req.Model),
		strings.TrimSpace(req.LLMProfile),
		timeout,
		strings.TrimSpace(req.TopicID),
		strings.TrimSpace(req.TopicTitle),
		strings.TrimSpace(req.WorkspaceDir),
		req.FileReferences,
		trigger,
	)
	if ownershipTransferred {
		releaseGeneration = false
	}
	return resp, err
}

func (r *consoleLocalRuntime) trySteerConsoleRun(task string, topicID string) runtimecontrol.SteerResult {
	if r == nil || r.runControl == nil || strings.TrimSpace(task) == "" {
		return runtimecontrol.SteerResult{}
	}
	return r.runControl.Steer("console", buildConsoleConversationKey(topicID), task)
}

func (r *consoleLocalRuntime) stopTask(_ context.Context, req daemonruntime.StopTaskRequest) (daemonruntime.StopTaskResponse, error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "/stop"
	}
	taskID := strings.TrimSpace(req.TaskID)
	topicID := strings.TrimSpace(req.TopicID)
	if taskID == "" && topicID == "" {
		return daemonruntime.StopTaskResponse{}, daemonruntime.BadRequest("task_id or topic_id is required")
	}

	result := runtimecontrol.StopResult{}
	if r != nil && r.runControl != nil {
		if taskID != "" {
			result = r.runControl.StopTask("console", taskID, reason)
		} else {
			result = r.runControl.Stop("console", buildConsoleConversationKey(topicID), reason)
		}
	}
	status := "not_found"
	if result.Found {
		status = "stopping"
	}
	return daemonruntime.StopTaskResponse{
		Status:   status,
		Found:    result.Found,
		TaskID:   taskID,
		TopicID:  topicID,
		Progress: strings.TrimSpace(result.Progress),
		Message:  runtimecontrol.StopFeedback(result.Found),
	}, nil
}

func (r *consoleLocalRuntime) currentApprovalGuard() *guard.Guard {
	if r == nil {
		return nil
	}
	generation := r.currentGeneration()
	if generation == nil || generation.bundle == nil || generation.bundle.taskRuntime == nil {
		return nil
	}
	return generation.bundle.taskRuntime.SharedGuard
}

func (r *consoleLocalRuntime) listApprovals(ctx context.Context, req daemonruntime.ApprovalListRequest) (daemonruntime.ApprovalListResponse, error) {
	if r == nil || r.store == nil {
		return daemonruntime.ApprovalListResponse{}, fmt.Errorf("task store is unavailable")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = string(daemonruntime.TaskPending)
	}
	if !strings.EqualFold(status, string(daemonruntime.TaskPending)) {
		return daemonruntime.ApprovalListResponse{}, daemonruntime.BadRequest("invalid status")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	tasks := r.store.List(daemonruntime.TaskListOptions{
		Status: daemonruntime.TaskPending,
		Limit:  limit,
	})
	now := time.Now().UTC()
	items := make([]daemonruntime.ApprovalInfo, 0, len(tasks))
	for _, task := range tasks {
		approvalID := strings.TrimSpace(task.ApprovalRequestID)
		if approvalID == "" {
			continue
		}
		g := r.currentApprovalGuard()
		if job, ok := r.pendingApproval(approvalID); ok {
			g = approvalGuardForGeneration(job.Generation)
		}
		if g == nil {
			continue
		}
		rec, ok, err := g.GetApproval(ctx, approvalID)
		if err != nil {
			return daemonruntime.ApprovalListResponse{}, err
		}
		if !ok || rec.Status != guard.ApprovalPending || (!rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt)) {
			continue
		}
		items = append(items, daemonruntime.ApprovalInfo{
			ApprovalRequestID:     approvalID,
			TaskID:                strings.TrimSpace(task.ID),
			RunID:                 strings.TrimSpace(rec.RunID),
			Status:                string(rec.Status),
			ToolName:              strings.TrimSpace(rec.ToolName),
			ActionSummaryRedacted: strings.TrimSpace(rec.ActionSummaryRedacted),
			Reasons:               append([]string(nil), rec.Reasons...),
			Runtime:               "console",
			Target:                "console",
			TopicID:               strings.TrimSpace(task.TopicID),
			CreatedAt:             rec.CreatedAt,
			ExpiresAt:             rec.ExpiresAt,
			PendingAt:             task.PendingAt,
		})
		if len(items) >= limit {
			break
		}
	}
	return daemonruntime.ApprovalListResponse{
		Items: items,
		Limit: limit,
	}, nil
}

func (r *consoleLocalRuntime) approveApproval(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
	approvalID := strings.TrimSpace(req.ApprovalRequestID)
	if approvalID == "" {
		return daemonruntime.ApprovalDecisionResponse{}, daemonruntime.BadRequest("approval_request_id is required")
	}
	claim, claimState, claimErr := r.claimPendingApproval(approvalID)
	if claimErr != nil {
		return daemonruntime.ApprovalDecisionResponse{}, claimErr
	}
	switch claimState {
	case runtimecore.PendingApprovalClaimInFlight:
		return daemonruntime.ApprovalDecisionResponse{}, runtimecore.ErrPendingApprovalClaimInFlight
	case runtimecore.PendingApprovalClaimMissing:
		taskID := r.taskIDForApproval(approvalID)
		return r.approvalResumeFailedResponse(approvalID, taskID, "pending approval handle is unavailable")
	}
	defer r.completePendingApprovalClaim(claim)
	job := claim.Job
	restored, err := r.resolveClaimedApproval(ctx, claim, guard.ApprovalApproved, req.Actor, req.Note)
	if err != nil {
		if !restored && job.Generation != nil {
			job.Generation.release()
		}
		return daemonruntime.ApprovalDecisionResponse{}, err
	}
	job.ResumeApprovalID = approvalID
	if r.runner == nil {
		if job.Generation != nil {
			job.Generation.release()
		}
		return r.approvalResumeFailedResponse(approvalID, job.TaskID, "task runner is unavailable")
	}
	resumedAt := time.Now().UTC()
	if err := r.taskStateUpdater().Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskQueued
		info.Error = ""
		info.ResumedAt = &resumedAt
		runtimecore.ClearTaskPendingApprovalFields(info)
	}); err != nil {
		failErr := r.markClaimedApprovalFailed(job.TaskID, approvalID, err)
		if job.Generation != nil {
			job.Generation.release()
		}
		return daemonruntime.ApprovalDecisionResponse{}, failErr
	}
	if err := r.runner.Enqueue(ctx, job.ConversationKey, func(version uint64) consoleLocalTaskJob {
		job.Version = version
		return job
	}); err != nil {
		if job.Generation != nil {
			job.Generation.release()
		}
		return r.approvalResumeFailedResponse(approvalID, job.TaskID, strings.TrimSpace(err.Error()))
	}
	if r.streamHub != nil {
		r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskQueued))
	}
	return daemonruntime.ApprovalDecisionResponse{
		ApprovalRequestID: approvalID,
		TaskID:            job.TaskID,
		Status:            string(guard.ApprovalApproved),
		Resumed:           true,
	}, nil
}

func (r *consoleLocalRuntime) denyApproval(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
	approvalID := strings.TrimSpace(req.ApprovalRequestID)
	if approvalID == "" {
		return daemonruntime.ApprovalDecisionResponse{}, daemonruntime.BadRequest("approval_request_id is required")
	}
	claim, claimState, claimErr := r.claimPendingApproval(approvalID)
	if claimErr != nil {
		return daemonruntime.ApprovalDecisionResponse{}, claimErr
	}
	switch claimState {
	case runtimecore.PendingApprovalClaimInFlight:
		return daemonruntime.ApprovalDecisionResponse{}, runtimecore.ErrPendingApprovalClaimInFlight
	case runtimecore.PendingApprovalClaimMissing:
		return daemonruntime.ApprovalDecisionResponse{}, fmt.Errorf("pending approval handle is unavailable")
	}
	defer r.completePendingApprovalClaim(claim)
	job := claim.Job
	taskID := strings.TrimSpace(job.TaskID)
	if taskID == "" {
		taskID = r.taskIDForApproval(approvalID)
	}
	restored, err := r.resolveClaimedApproval(ctx, claim, guard.ApprovalDenied, req.Actor, req.Note)
	if err != nil {
		if !restored && job.Generation != nil {
			job.Generation.release()
		}
		return daemonruntime.ApprovalDecisionResponse{}, err
	}
	if job.Generation != nil {
		defer job.Generation.release()
	}
	if taskID != "" {
		finishedAt := time.Now().UTC()
		if err := r.store.Update(taskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskCanceled
			info.Error = "Approval denied. Task canceled."
			info.FinishedAt = &finishedAt
			runtimecore.ClearTaskPendingApprovalFields(info)
		}); err != nil {
			return daemonruntime.ApprovalDecisionResponse{}, err
		}
		if r.streamHub != nil {
			r.streamHub.PublishStatus(taskID, string(daemonruntime.TaskCanceled))
		}
	}
	return daemonruntime.ApprovalDecisionResponse{
		ApprovalRequestID: approvalID,
		TaskID:            taskID,
		Status:            string(guard.ApprovalDenied),
		Resumed:           false,
	}, nil
}

func approvalDecisionError(err error) error {
	if errors.Is(err, guard.ErrApprovalNotFound) {
		return daemonruntime.BadRequest("approval not found")
	}
	if errors.Is(err, guard.ErrApprovalNotPending) {
		return daemonruntime.BadRequest("approval is not pending")
	}
	return err
}

func (r *consoleLocalRuntime) taskStateUpdater() daemonruntime.TaskUpdater {
	if r == nil {
		return nil
	}
	if r.taskUpdater != nil {
		return r.taskUpdater
	}
	return r.store
}

func (r *consoleLocalRuntime) markClaimedApprovalFailed(taskID, approvalID string, cause error) error {
	taskID = strings.TrimSpace(taskID)
	applied, updateErr := runtimecore.FailPendingApprovalTask(r.taskStateUpdater(), taskID, approvalID, "approval resolution failed: "+strings.TrimSpace(cause.Error()))
	if updateErr != nil {
		return errors.Join(cause, updateErr)
	}
	if !applied {
		return errors.Join(cause, runtimecore.ErrApprovalTaskStateUnchanged)
	}
	if r.streamHub != nil {
		r.streamHub.PublishStatus(taskID, string(daemonruntime.TaskFailed))
	}
	return cause
}

func (r *consoleLocalRuntime) approvalResumeFailedResponse(approvalID, taskID, msg string) (daemonruntime.ApprovalDecisionResponse, error) {
	approvalID = strings.TrimSpace(approvalID)
	taskID = strings.TrimSpace(taskID)
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "approval resume failed"
	}
	displayErr := "approval resume failed: " + msg
	if taskID != "" && r != nil && r.store != nil {
		finishedAt := time.Now().UTC()
		if err := r.store.Update(taskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskFailed
			info.Error = displayErr
			info.FinishedAt = &finishedAt
			runtimecore.ClearTaskPendingApprovalFields(info)
		}); err != nil {
			return daemonruntime.ApprovalDecisionResponse{}, err
		}
		if r.streamHub != nil {
			r.streamHub.PublishStatus(taskID, string(daemonruntime.TaskFailed))
		}
	}
	return daemonruntime.ApprovalDecisionResponse{
		ApprovalRequestID: approvalID,
		TaskID:            taskID,
		Status:            string(guard.ApprovalApproved),
		Resumed:           false,
		Error:             displayErr,
	}, nil
}

func (r *consoleLocalRuntime) resolveClaimedApproval(ctx context.Context, claim runtimecore.PendingApprovalClaim[consoleLocalTaskJob], status guard.ApprovalStatus, actor, note string) (bool, error) {
	approvalID := strings.TrimSpace(claim.ID)
	job := claim.Job
	taskID := strings.TrimSpace(job.TaskID)
	if taskID == "" {
		taskID = r.taskIDForApproval(approvalID)
	}
	failClaimed := func(cause error) (bool, error) {
		return false, r.markClaimedApprovalFailed(taskID, approvalID, cause)
	}
	cancelClaimed := func(cause error) (bool, error) {
		var updateErr error
		if taskID != "" && r.store != nil {
			finishedAt := time.Now().UTC()
			updateErr = r.store.Update(taskID, func(info *daemonruntime.TaskInfo) {
				info.Status = daemonruntime.TaskCanceled
				info.Error = "Approval denied. Task canceled."
				info.FinishedAt = &finishedAt
				runtimecore.ClearTaskPendingApprovalFields(info)
			})
			if updateErr == nil && r.streamHub != nil {
				r.streamHub.PublishStatus(taskID, string(daemonruntime.TaskCanceled))
			}
		}
		if updateErr != nil {
			return false, errors.Join(cause, updateErr)
		}
		return false, cause
	}
	g := approvalGuardForGeneration(job.Generation)
	if g == nil {
		return failClaimed(fmt.Errorf("approvals are unavailable"))
	}
	rec, ok, err := g.GetApproval(ctx, approvalID)
	if err != nil {
		return failClaimed(err)
	}
	if !ok {
		return failClaimed(daemonruntime.BadRequest("approval not found"))
	}
	if rec.Status != guard.ApprovalPending {
		if status == guard.ApprovalDenied && rec.Status == guard.ApprovalDenied {
			return cancelClaimed(daemonruntime.BadRequest("approval is not pending"))
		}
		return failClaimed(daemonruntime.BadRequest("approval is not pending"))
	}
	if !rec.ExpiresAt.IsZero() && time.Now().UTC().After(rec.ExpiresAt) {
		if err := runtimecore.ExpirePendingApproval(ctx, g, r.store, approvalID, job.TaskID, "console:expiry"); err != nil {
			if !errors.Is(err, guard.ErrApprovalNotPending) {
				task, taskExists := r.store.Get(taskID)
				if taskExists && task.Status == daemonruntime.TaskPending && strings.TrimSpace(task.ApprovalRequestID) == approvalID {
					return failClaimed(err)
				}
				return false, err
			}
			return false, daemonruntime.BadRequest("approval is expired")
		}
		if r.streamHub != nil {
			r.streamHub.PublishStatus(taskID, string(daemonruntime.TaskCanceled))
		}
		return false, daemonruntime.BadRequest("approval is expired")
	}
	commitState, pendingRec, resolveErr := runtimecore.ResolveApprovalCommit(ctx, g, approvalID, status, actor, note)
	if resolveErr != nil {
		switch commitState {
		case runtimecore.ApprovalCommitPending:
			if r.consoleExecutionState == nil {
				return failClaimed(errors.Join(resolveErr, runtimecore.ErrPendingApprovalRegistryUnavailable))
			}
			restoreErr := r.consoleExecutionState.restorePendingApprovalClaim(claim, pendingRec.ExpiresAt)
			if restoreErr != nil {
				return failClaimed(errors.Join(resolveErr, restoreErr))
			}
			return true, approvalDecisionError(resolveErr)
		case runtimecore.ApprovalCommitCommitted:
			if status == guard.ApprovalDenied {
				return cancelClaimed(resolveErr)
			}
			return failClaimed(resolveErr)
		default:
			return failClaimed(resolveErr)
		}
	}
	return false, nil
}

func (r *consoleLocalRuntime) registerPendingApproval(approvalID string, job consoleLocalTaskJob) error {
	if r == nil {
		return fmt.Errorf("console runtime is unavailable")
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return fmt.Errorf("approval id is required")
	}
	g := approvalGuardForGeneration(job.Generation)
	if g == nil {
		return fmt.Errorf("approvals are unavailable")
	}
	rec, ok, err := g.GetApproval(context.Background(), approvalID)
	if err != nil || !ok {
		if err == nil {
			err = guard.ErrApprovalNotFound
		}
		return err
	}
	if job.Generation != nil {
		job.Generation.acquire()
	}
	if r.consoleExecutionState == nil {
		if job.Generation != nil {
			job.Generation.release()
		}
		return runtimecore.ErrPendingApprovalRegistryUnavailable
	}
	err = r.consoleExecutionState.addPendingApproval(approvalID, job, rec.ExpiresAt)
	if err != nil {
		if job.Generation != nil {
			job.Generation.release()
		}
		if errors.Is(err, errConsoleExecutionClosed) {
			return runtimecore.ErrPendingApprovalRegistryUnavailable
		}
		return err
	}
	return nil
}

func (r *consoleLocalRuntime) expirePendingApproval(claim runtimecore.PendingApprovalClaim[consoleLocalTaskJob]) {
	approvalID := strings.TrimSpace(claim.ID)
	job := claim.Job
	defer r.completePendingApprovalClaim(claim)
	releaseGeneration := true
	defer func() {
		if releaseGeneration && job.Generation != nil {
			job.Generation.release()
		}
	}()
	g := approvalGuardForGeneration(job.Generation)
	if err := runtimecore.ExpirePendingApproval(context.Background(), g, r.taskStateUpdater(), approvalID, job.TaskID, "console:expiry"); err != nil {
		if errors.Is(err, runtimecore.ErrApprovalCommitIndeterminate) || errors.Is(err, runtimecore.ErrApprovalTaskFinalizationFailed) {
			var restoreErr error
			if r.consoleExecutionState == nil {
				restoreErr = runtimecore.ErrPendingApprovalRegistryUnavailable
			} else {
				restoreErr = r.consoleExecutionState.restorePendingApprovalClaim(claim, time.Now().Add(runtimecore.PendingApprovalRetryDelay))
			}
			if restoreErr == nil {
				releaseGeneration = false
				r.currentLogger().Warn("console_approval_expiry_retry", "approval_request_id", approvalID, "task_id", job.TaskID, "error", err.Error())
				return
			}
			failed, failErr := runtimecore.FailPendingApprovalTask(
				r.taskStateUpdater(),
				job.TaskID,
				approvalID,
				runtimecore.ApprovalExpiryResolutionFailedTaskError,
			)
			if failed && r.streamHub != nil {
				r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskFailed))
			}
			err = errors.Join(err, restoreErr, failErr)
		}
		if !errors.Is(err, guard.ErrApprovalNotPending) {
			r.currentLogger().Error("console_approval_expiry_error", "approval_request_id", approvalID, "task_id", job.TaskID, "error", err.Error())
		}
		return
	}
	if r.streamHub != nil {
		r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskCanceled))
	}
}

func (r *consoleLocalRuntime) closePendingApproval(approvalID string, job consoleLocalTaskJob) {
	if job.Generation != nil {
		defer job.Generation.release()
	}
	approvalID = strings.TrimSpace(approvalID)
	_, _, resolveErr := runtimecore.ResolveApprovalCommit(
		context.Background(),
		approvalGuardForGeneration(job.Generation),
		approvalID,
		guard.ApprovalExpired,
		consoleApprovalShutdownActor,
		consoleApprovalShutdownComment,
	)
	if resolveErr != nil && !errors.Is(resolveErr, guard.ErrApprovalNotPending) {
		r.currentLogger().Error("console_approval_close_resolve_error", "approval_request_id", approvalID, "task_id", job.TaskID, "error", resolveErr.Error())
	}
	applied, err := runtimecore.FailPendingApprovalTask(
		r.taskStateUpdater(),
		job.TaskID,
		approvalID,
		consoleRuntimeClosedTaskError,
	)
	if err != nil {
		r.currentLogger().Error("console_approval_close_error", "approval_request_id", approvalID, "task_id", job.TaskID, "error", err.Error())
		return
	}
	if applied && r.streamHub != nil {
		r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskFailed))
	}
}

func approvalGuardForGeneration(generation *consoleLocalRuntimeGeneration) *guard.Guard {
	if generation == nil || generation.bundle == nil || generation.bundle.taskRuntime == nil {
		return nil
	}
	return generation.bundle.taskRuntime.SharedGuard
}

func (r *consoleLocalRuntime) taskIDForApproval(approvalID string) string {
	if r == nil || r.store == nil {
		return ""
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return ""
	}
	items := r.store.List(daemonruntime.TaskListOptions{
		Status: daemonruntime.TaskPending,
		Limit:  200,
	})
	for _, item := range items {
		if strings.TrimSpace(item.ApprovalRequestID) == approvalID {
			return strings.TrimSpace(item.ID)
		}
	}
	return ""
}

func (r *consoleLocalRuntime) handleConsoleRuntimeCommand(generation *consoleLocalRuntimeGeneration, req daemonruntime.SubmitTaskRequest, timeout time.Duration, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, bool, error) {
	task := strings.TrimSpace(req.Task)
	cmdWord, _ := chatcommands.ParseCommand(task)
	normalizedCmd := chatcommands.NormalizeCommand(cmdWord)
	if normalizedCmd == "" {
		return daemonruntime.SubmitTaskResponse{}, false, nil
	}
	topicID := strings.TrimSpace(req.TopicID)
	if normalizedCmd == "/stop" {
		conversationKey := buildConsoleConversationKey(topicID)
		result := runtimecontrol.StopResult{}
		if r != nil && r.runControl != nil {
			result = r.runControl.Stop("console", conversationKey, "/stop")
		}
		output := runtimecontrol.StopFeedback(result.Found)
		resp, submitErr := r.submitSyntheticTask(generation, task, output, "", timeout, topicID, strings.TrimSpace(req.TopicTitle), strings.TrimSpace(req.WorkspaceDir), trigger)
		return resp, true, submitErr
	}
	if (normalizedCmd == "/ctx" || normalizedCmd == "/workspace") && topicID == "" {
		return daemonruntime.SubmitTaskResponse{}, true, daemonruntime.BadRequest("topic_id is required for " + normalizedCmd)
	}
	if chatcommands.IsContextCompactCommand(task) {
		return daemonruntime.SubmitTaskResponse{}, false, nil
	}
	store := r.currentWorkspaceStore()
	if normalizedCmd == "/workspace" && store == nil {
		return daemonruntime.SubmitTaskResponse{}, true, fmt.Errorf("workspace store is not configured")
	}
	conversationKey := buildConsoleConversationKey(topicID)
	reg := chatcommands.NewRuntimeRegistry(chatcommands.RuntimeRegistryOptions{
		ModelCommand: func(text string) (string, bool, error) {
			return llmselect.ExecuteCommandText(
				generation.llmValues,
				llmselect.ProcessStore(),
				text,
			)
		},
		SkillCommand: func() (string, error) {
			return skillsutil.RenderSkillStatus(skillsutil.SkillsConfigFromReader(generation.reader), nil)
		},
		ContextCommand: topiccontext.NewStore(generation.paths.TopicContextPath).CommandFunc(conversationKey),
		WorkspaceStore: store,
		WorkspaceKey:   conversationKey,
	})
	result, handled, err := reg.Dispatch(context.Background(), task)
	if !handled {
		return daemonruntime.SubmitTaskResponse{}, false, nil
	}
	output := ""
	if err != nil {
		output = "error: " + strings.TrimSpace(err.Error())
	} else if result != nil {
		output = strings.TrimSpace(result.Reply)
	}
	workspaceDir := strings.TrimSpace(req.WorkspaceDir)
	if normalizedCmd == "/workspace" {
		workspaceDir = ""
	}
	resp, submitErr := r.submitSyntheticTask(generation, task, output, "", timeout, topicID, strings.TrimSpace(req.TopicTitle), workspaceDir, trigger)
	return resp, true, submitErr
}

func buildConsoleStopProgress(plan *consolePlanProgress, activity *consoleActivityProgress) string {
	items := make([]string, 0, 4)
	if plan != nil && len(plan.Steps) > 0 {
		planTotal := len(plan.Steps)
		planCompleted := 0
		planCurrent := ""
		for _, step := range plan.Steps {
			status := strings.TrimSpace(step.Status)
			if status == agent.PlanStatusCompleted {
				planCompleted++
				continue
			}
			if planCurrent == "" {
				planCurrent = strings.TrimSpace(step.Step)
			}
		}
		if planCurrent == "" && planCompleted < planTotal {
			for _, step := range plan.Steps {
				if strings.TrimSpace(step.Step) != "" {
					planCurrent = strings.TrimSpace(step.Step)
					break
				}
			}
		}
		items = append(items, fmt.Sprintf("计划 %d/%d", planCompleted, planTotal))
		if planCurrent != "" {
			items = append(items, "当前步骤 "+planCurrent)
		}
	}
	if activity != nil {
		toolCalls := 0
		for _, entry := range activity.History {
			if strings.TrimSpace(entry.Kind) == "tool" {
				toolCalls++
			}
		}
		if toolCalls > 0 {
			items = append(items, fmt.Sprintf("工具调用 %d", toolCalls))
		}
		if current := activity.Current; current != nil {
			parts := []string{strings.TrimSpace(current.Kind), strings.TrimSpace(current.Name), strings.TrimSpace(current.Status)}
			if currentActivity := strings.TrimSpace(strings.Join(nonEmptyStrings(parts), " ")); currentActivity != "" {
				items = append(items, "当前活动 "+currentActivity)
			}
		}
	}
	return strings.Join(items, "，")
}

func nonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (r *consoleLocalRuntime) submitSyntheticTask(generation *consoleLocalRuntimeGeneration, task string, output string, steerTargetTaskID string, timeout time.Duration, topicID string, topicTitle string, workspaceDir string, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, error) {
	job, _, err := r.acceptTask(generation, task, "", "", timeout, topicID, topicTitle, workspaceDir, nil, trigger)
	if err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	steerTargetTaskID = strings.TrimSpace(steerTargetTaskID)
	finishedAt := time.Now().UTC()
	if err := r.taskStateUpdater().Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.FinishedAt = &finishedAt
		info.SteerTargetTaskID = steerTargetTaskID
		info.Result = map[string]any{
			"final": map[string]any{
				"output": strings.TrimSpace(output),
			},
		}
	}); err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	return daemonruntime.SubmitTaskResponse{
		ID:                job.TaskID,
		Status:            daemonruntime.TaskDone,
		TopicID:           job.TopicID,
		SteerTargetTaskID: steerTargetTaskID,
	}, nil
}

func (r *consoleLocalRuntime) enqueueTask(ctx context.Context, task string, model string, timeout time.Duration, topicID string, topicTitle string, trigger daemonruntime.TaskTrigger) (daemonruntime.SubmitTaskResponse, error) {
	generation, err := r.captureGeneration()
	if err != nil {
		return daemonruntime.SubmitTaskResponse{}, err
	}
	job, resp, err := r.acceptTask(generation, task, model, "", timeout, topicID, topicTitle, "", nil, trigger)
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
		if stateErr := runtimecore.MarkTaskFailed(r.store, job.TaskID, strings.TrimSpace(err.Error()), taskdomain.EndedByCancellation(ctx, err)); stateErr != nil {
			return daemonruntime.SubmitTaskResponse{}, fmt.Errorf("enqueue console task: %v; persist failed state: %w", err, stateErr)
		}
		return daemonruntime.SubmitTaskResponse{}, err
	}
	return resp, nil
}

func (r *consoleLocalRuntime) handleTaskJob(workerCtx context.Context, conversationKey string, job consoleLocalTaskJob) {
	if r == nil {
		return
	}
	if job.Generation == nil {
		if err := runtimecore.MarkTaskFailed(r.store, job.TaskID, "console task generation is not initialized", false); err != nil {
			r.currentLogger().Error("console_task_state_write_error", "task_id", job.TaskID, "status", daemonruntime.TaskFailed, "error", err.Error())
		}
		return
	}
	defer job.Generation.release()
	logger := job.Generation.logger
	if logger == nil {
		logger = r.currentLogger()
	}
	traceID := strings.TrimSpace(job.Trigger.TraceID)
	if traceID == "" {
		traceID = job.TaskID
	}
	logger = logger.With("task_id", job.TaskID, "trace_id", traceID)
	if err := runtimecore.MarkTaskRunning(r.store, job.TaskID); err != nil {
		logger.Error("console_task_state_write_error", "status", daemonruntime.TaskRunning, "error", err.Error())
		return
	}
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

	replySink := newConsoleReplySink(r.streamHub, job.TaskID, logger, approvalGuardForGeneration(job.Generation))
	var progressMu sync.Mutex
	var latestPlan *consolePlanProgress
	var latestActivity *consoleActivityProgress
	eventSink := newConsoleEventPreviewSink(r.streamHub, job.TaskID, logger, approvalGuardForGeneration(job.Generation))
	eventSink.activityUpdated = func(progress *consoleActivityProgress) {
		progressMu.Lock()
		latestActivity = cloneConsoleActivityProgress(progress)
		progressMu.Unlock()
	}
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

	planStepUpdate := func(runCtx *agent.Context, _ agent.PlanStepUpdate) {
		progress := buildConsolePlanProgress(consoleTaskPlan(nil, runCtx))
		if progress == nil {
			return
		}
		progressMu.Lock()
		latestPlan = cloneConsolePlanProgress(progress)
		progressMu.Unlock()
		if r.streamHub != nil {
			r.streamHub.PublishPlan(job.TaskID, progress)
		}
	}

	snapshot := func() string {
		progressMu.Lock()
		plan := cloneConsolePlanProgress(latestPlan)
		activity := cloneConsoleActivityProgress(latestActivity)
		progressMu.Unlock()
		return buildConsoleStopProgress(plan, activity)
	}
	var steerSource agent.SteerSource
	var lease *runtimecontrol.RunLease
	var runCtx context.Context
	if r.runControl != nil {
		var err error
		lease, err = r.runControl.StartLease(workerCtx, job.Timeout, runtimecontrol.ActiveRun{
			Runtime:         "console",
			ConversationKey: conversationKey,
			TopicID:         job.TopicID,
			TaskID:          job.TaskID,
			RunID:           job.TaskID,
			Snapshot:        snapshot,
			EventSink:       eventSink,
		})
		if err != nil {
			eventSink.Close()
			displayErr := strings.TrimSpace(err.Error())
			if stateErr := runtimecore.MarkTaskFailed(r.store, job.TaskID, displayErr, false); stateErr != nil {
				logger.Error("console_task_state_write_error", "status", daemonruntime.TaskFailed, "error", stateErr.Error())
			}
			_ = replySink.Abort(context.Background(), errors.New(displayErr))
			streamTracker.LogSummary("failed")
			return
		}
		runCtx = lease.Context
		steerSource = lease.SteerQueue
	} else {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(workerCtx, job.Timeout)
		defer cancel()
	}
	if runCtx == nil {
		runCtx = workerCtx
	}
	runCtx = agent.WithEventSinkContext(runCtx, eventSink)

	final, agentCtx, runErr := r.runTask(runCtx, conversationKey, job, onStream, steerSource, planStepUpdate)
	contextCanceled := taskdomain.EndedByCancellation(runCtx, runErr)
	userStopped := false
	if lease != nil {
		userStopped = lease.UserStopped()
		lease.Finish()
	}

	if runErr != nil {
		eventSink.Close()
		displayErr := strings.TrimSpace(outputfmt.FormatErrorForDisplay(runErr))
		if displayErr == "" {
			displayErr = strings.TrimSpace(runErr.Error())
		}
		if userStopped {
			displayErr = "stopped by user"
		}
		finishedAt := time.Now().UTC()
		status := daemonruntime.TaskFailed
		if contextCanceled || userStopped {
			status = daemonruntime.TaskCanceled
		}
		if stateErr := r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
			info.Status = status
			info.Error = displayErr
			info.FinishedAt = &finishedAt
			progressMu.Lock()
			activity := cloneConsoleActivityProgress(latestActivity)
			progressMu.Unlock()
			info.Result = buildConsoleTaskResult(final, agentCtx, activity)
		}); stateErr != nil {
			logger.Error("console_task_state_write_error", "status", status, "error", stateErr.Error())
		}
		_ = replySink.Abort(context.Background(), errors.New(displayErr))
		streamTracker.LogSummary("failed")
		if userStopped && r.streamHub != nil {
			r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskCanceled))
		}
		return
	}

	if pendingID, ok := runtimecore.PendingApprovalID(final); ok {
		eventSink.Close()
		pendingAt := time.Now().UTC()
		if err := r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskPending
			info.PendingAt = &pendingAt
			info.ApprovalRequestID = pendingID
			progressMu.Lock()
			activity := cloneConsoleActivityProgress(latestActivity)
			progressMu.Unlock()
			info.Result = buildConsoleTaskResult(final, agentCtx, activity)
		}); err != nil {
			logger.Error("console_task_state_write_error", "status", daemonruntime.TaskPending, "error", err.Error())
			_ = replySink.Abort(context.Background(), err)
			streamTracker.LogSummary("failed")
			return
		}
		if err := r.registerPendingApproval(pendingID, job); err != nil {
			applied, stateErr := runtimecore.FailPendingApprovalTask(r.store, job.TaskID, pendingID, runtimecore.ApprovalRegistrationFailedTaskError)
			if stateErr != nil {
				err = errors.Join(err, stateErr)
			}
			logger.Error("console_approval_register_error", "approval_request_id", pendingID, "task_id", job.TaskID, "task_failed", applied, "error", err.Error())
			if applied && r.streamHub != nil {
				r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskFailed))
			}
			_ = replySink.Abort(context.Background(), err)
			streamTracker.LogSummary("failed")
			return
		}
		if r.streamHub != nil {
			r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskPending))
		}
		streamTracker.LogSummary("pending")
		return
	}

	finishedAt := time.Now().UTC()
	output := strings.TrimSpace(outputfmt.FormatFinalOutput(final))
	eventSink.Close()
	if err := r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.FinishedAt = &finishedAt
		progressMu.Lock()
		activity := cloneConsoleActivityProgress(latestActivity)
		progressMu.Unlock()
		info.Result = buildConsoleTaskResult(final, agentCtx, activity)
	}); err != nil {
		logger.Error("console_task_state_write_error", "status", daemonruntime.TaskDone, "error", err.Error())
		_ = replySink.Abort(context.Background(), err)
		streamTracker.LogSummary("failed")
		return
	}
	_ = replySink.Finalize(context.Background(), output)
	streamTracker.LogSummary("done")
	if !chatcommands.IsContextCompactCommand(job.Task) {
		r.maybeRefreshTopicTitle(job, output)
	}
}

func (r *consoleLocalRuntime) handleDroppedTaskJob(_ string, job consoleLocalTaskJob) {
	if job.Generation != nil {
		defer job.Generation.release()
	}
	if r == nil {
		return
	}
	finishedAt := time.Now().UTC()
	if err := r.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskCanceled
		info.Error = consoleRuntimeClosedTaskError
		info.FinishedAt = &finishedAt
		runtimecore.ClearTaskPendingApprovalFields(info)
	}); err != nil {
		r.currentLogger().Error(
			"console_task_state_write_error",
			"task_id", job.TaskID,
			"status", daemonruntime.TaskCanceled,
			"error", err.Error(),
		)
		return
	}
	if r.streamHub != nil {
		r.streamHub.PublishStatus(job.TaskID, string(daemonruntime.TaskCanceled))
	}
}

func (r *consoleLocalRuntime) handleTaskJobPanic(conversationKey string, job consoleLocalTaskJob) {
	if r == nil {
		return
	}
	taskID := strings.TrimSpace(job.TaskID)
	if taskID == "" {
		return
	}
	if strings.TrimSpace(conversationKey) == "" {
		conversationKey = job.ConversationKey
	}
	if r.runControl != nil {
		r.runControl.Finish("console", conversationKey, taskID)
	}
	if r.store == nil {
		return
	}
	finishedAt := time.Now().UTC()
	if err := r.store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		if info == nil || (info.Status != daemonruntime.TaskQueued && info.Status != daemonruntime.TaskRunning) {
			return
		}
		info.Status = daemonruntime.TaskFailed
		info.Error = consoleWorkerPanicTaskError
		info.FinishedAt = &finishedAt
	}); err != nil {
		r.currentLogger().Error("console_task_state_write_error", "task_id", taskID, "status", daemonruntime.TaskFailed, "error", err.Error())
	}
}

func (r *consoleLocalRuntime) runTask(ctx context.Context, conversationKey string, job consoleLocalTaskJob, onStream llm.StreamHandler, steerSource agent.SteerSource, planStepUpdate func(*agent.Context, agent.PlanStepUpdate)) (*agent.Final, *agent.Context, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("console runtime is not initialized")
	}
	if job.Generation == nil {
		return nil, nil, fmt.Errorf("console task generation is not initialized")
	}
	generation := job.Generation
	ctx = llmstats.WithRunID(ctx, job.TaskID)
	ctx = topiccontext.WithScope(ctx, topiccontext.Scope{
		Runtime:         "console",
		ConversationKey: job.ConversationKey,
		TopicID:         job.TopicID,
	})
	ctx = pathroots.WithWorkspaceDir(ctx, job.WorkspaceDir)
	fileCacheDir := consoleFileCacheDir(generation.reader)
	validatedFileReferences, err := validateConsoleFileReferences(
		job.FileReferences,
		job.WorkspaceDir,
		fileCacheDir,
	)
	if err != nil {
		return nil, nil, err
	}
	job.FileReferences = validatedFileReferences
	task := strings.TrimSpace(job.Task)
	routePurpose := ""
	reasoningEffort := ""
	if thinkTask, ok := chatcommands.ExtractThinkTask(task); ok {
		task = strings.TrimSpace(thinkTask)
		job.Task = task
		routePurpose = llmutil.RoutePurposeThink
		reasoningEffort = llmutil.ReasoningEffortXHigh
	}
	if task == "" {
		return nil, nil, fmt.Errorf("empty console task")
	}
	bundle := generation.bundle
	if bundle == nil || bundle.taskRuntime == nil {
		return nil, nil, fmt.Errorf("console task runtime is not initialized")
	}
	var selectedRoute llmutil.ResolvedRoute
	if job.Route != nil {
		selectedRoute = *job.Route
	} else {
		selectedRoute, err = resolveConsoleTaskRoute(ctx, generation, bundle.taskRuntime, routePurpose, job.LLMProfile)
		if err != nil {
			return nil, nil, err
		}
	}
	if reasoningEffort != "" {
		selectedRoute = llmutil.ResolvedRouteWithReasoningEffort(selectedRoute, reasoningEffort)
	}
	model := strings.TrimSpace(job.Model)
	if model == "" || routePurpose == llmutil.RoutePurposeThink {
		model = strings.TrimSpace(selectedRoute.ClientConfig.Model)
	}
	imagePaths, err := resolveConsoleImageReferencePaths(job.FileReferences, job.WorkspaceDir, fileCacheDir)
	if err != nil {
		return nil, nil, err
	}
	checkpointHistory, err := bundle.taskRuntime.PrepareContextHistory(
		ctx,
		conversationKey,
		r.loadConsoleTopicHistory(job),
		newConsoleInboundHistoryItem(job),
	)
	if err != nil {
		return nil, nil, err
	}
	historyMsgs, currentMsg, err := renderConsolePromptMessages(checkpointHistory.History, job, model, selectedRoute.Values.SupportsImageParts, imagePaths, generation.logger)
	if err != nil {
		return nil, nil, err
	}
	var historyBoundaries []string
	if len(historyMsgs) > 0 {
		historyBoundaries = []string{checkpointHistory.HistoryBoundary}
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
			return generation.memRuntime.Orchestrator.Record(buildConsoleMemoryRecordRequest(job, memSubjectID, finalOutput))
		}
		memoryHooks.NotifyRecorded = func() {
			if generation.memRuntime.ProjectionWorker != nil {
				generation.memRuntime.ProjectionWorker.NotifyRecordAppended()
			}
		}
	}
	traceID := strings.TrimSpace(job.Trigger.TraceID)
	if traceID == "" {
		traceID = job.TaskID
	}
	meta := map[string]any{
		"trigger":          consoleTriggerSource(job.Trigger),
		"console_task_id":  job.TaskID,
		"console_topic_id": strings.TrimSpace(job.TopicID),
	}
	meta = taskruntime.ApplyObservationMeta(meta, taskruntime.ObservationMetaIDs{
		TaskID:  job.TaskID,
		TraceID: traceID,
		TopicID: job.TopicID,
	})
	if pokeMeta := job.WakeSignal.MetaValue(); pokeMeta != nil {
		meta["poke"] = pokeMeta
	}
	promptAugment := func(spec *agent.PromptSpec, reg *tools.Registry) {
		toolsutil.SetTodoUpdateToolAddContext(reg, todo.AddResolveContext{
			Channel:         "console",
			ChatType:        "topic",
			SpeakerUsername: consoleParticipantKey,
			UserInputRaw:    job.Task,
		})
		prefixBlocks := make([]agent.PromptBlock, 0, 3)
		if block := workspace.PromptBlock(job.WorkspaceDir); strings.TrimSpace(block.Content) != "" {
			prefixBlocks = append(prefixBlocks, block)
		}
		if block, err := consoleArtifactPreviewPromptBlock(job.WorkspaceDir); err == nil && strings.TrimSpace(block.Content) != "" {
			prefixBlocks = append(prefixBlocks, block)
		} else if err != nil && generation.logger != nil {
			generation.logger.Warn("console_artifact_preview_prompt_render_failed", "error", err.Error())
		}
		if block := consoleFileReferencesPromptBlock(job.FileReferences); strings.TrimSpace(block.Content) != "" {
			prefixBlocks = append(prefixBlocks, block)
		}
		if len(prefixBlocks) > 0 {
			spec.Blocks = append(prefixBlocks, spec.Blocks...)
		}
		if !job.WakeSignal.IsZero() {
			promptprofile.AppendWakeSignalBlock(spec, job.WakeSignal.Normalize())
		}
		promptprofile.AppendConsoleRuntimeBlocks(spec)
	}
	reactTool := newConsoleMessageReactTool()
	reg := bundle.taskRuntime.BaseRegistry.Clone()
	if err := reg.Replace(reactTool); err != nil {
		return nil, nil, err
	}
	imageToolScope := strings.TrimSpace(job.ConversationKey)
	if imageToolScope == "" && strings.TrimSpace(job.TopicID) != "" {
		imageToolScope = "console:" + strings.TrimSpace(job.TopicID)
	}
	runReq := taskruntime.RunRequest{
		Task:                    task,
		Model:                   model,
		Route:                   &selectedRoute,
		LLMProfile:              strings.TrimSpace(job.LLMProfile),
		RoutePurpose:            routePurpose,
		ReasoningEffortOverride: reasoningEffort,
		Scene:                   "console.loop",
		History:                 historyMsgs,
		CurrentMessage:          currentMsg,
		Registry:                reg,
		OnStream:                onStream,
		SteerSource:             steerSource,
		Meta:                    meta,
		PromptAugment:           promptAugment,
		PlanStepUpdate:          planStepUpdate,
		Memory:                  memoryHooks,
		ImageToolScope:          imageToolScope,
		ImageToolRetention:      toolsutil.ImageToolRetentionSticky,
		ContextCheckpointStore:  checkpointHistory.Store,
		HistoryBoundaries:       historyBoundaries,
		CurrentMessageBoundary:  checkpointHistory.CurrentMessageBoundary,
	}
	var result taskruntime.RunResult
	var runErr error
	if approvalID := strings.TrimSpace(job.ResumeApprovalID); approvalID != "" {
		result, runErr = bundle.taskRuntime.Resume(ctx, approvalID, runReq)
	} else {
		result, runErr = bundle.taskRuntime.Run(ctx, runReq)
	}
	if runErr != nil {
		return result.Final, result.Context, runErr
	}
	result.Final = applyConsoleMessageReactionFinal(result.Final, reactTool.LastEmoji())
	return result.Final, result.Context, nil
}

func resolveConsoleTaskRoute(ctx context.Context, generation *consoleLocalRuntimeGeneration, taskRuntime *taskruntime.Runtime, routePurpose string, profile string) (llmutil.ResolvedRoute, error) {
	routePurpose = strings.TrimSpace(routePurpose)
	if routePurpose == "" {
		routePurpose = llmutil.RoutePurposeMainLoop
	}
	var (
		route llmutil.ResolvedRoute
		err   error
	)
	if profile = strings.TrimSpace(profile); profile != "" {
		if generation == nil || generation.commonDeps.ResolveLLMRouteWithProfile == nil {
			return llmutil.ResolvedRoute{}, fmt.Errorf("llm profile selection is unavailable")
		}
		route, err = generation.commonDeps.ResolveLLMRouteWithProfile(routePurpose, profile)
	} else {
		if taskRuntime == nil {
			return llmutil.ResolvedRoute{}, fmt.Errorf("console task runtime is not initialized")
		}
		route, err = taskRuntime.ResolveRouteForRun(ctx, routePurpose)
	}
	if err != nil {
		return llmutil.ResolvedRoute{}, err
	}
	selectionKey := strings.TrimSpace(llmstats.RunIDFromContext(ctx))
	if selectionKey == "" {
		selectionKey = strings.TrimSpace(llmstats.OriginEventIDFromContext(ctx))
	}
	return llmutil.SelectRouteCandidate(route, selectionKey), nil
}

func buildConsoleTaskResult(final *agent.Final, runCtx *agent.Context, activity *consoleActivityProgress) map[string]any {
	out := map[string]any{
		"final": final,
	}
	if plan := buildConsolePlanProgress(consoleTaskPlan(final, runCtx)); plan != nil {
		out["plan"] = plan
	}
	if activity != nil {
		out["activity"] = cloneConsoleActivityProgress(activity)
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
	if topicID == "" || topicID == daemonruntime.ConsoleDefaultTopicID || topicID == daemonruntime.ConsoleAwarenessTopicID {
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
	if ctx == nil {
		ctx = context.Background()
	}
	route, err := generation.commonDeps.ResolveLLMRoute(llmutil.RoutePurposeMainLoop)
	if err != nil {
		return "", err
	}
	selectionKey := strings.TrimSpace(llmstats.RunIDFromContext(ctx))
	if selectionKey == "" {
		selectionKey = strings.TrimSpace(llmstats.OriginEventIDFromContext(ctx))
	}
	if selectionKey == "" {
		selectionKey = llmstats.NewSyntheticRunID("console-topic-title")
		ctx = llmstats.WithRunID(ctx, selectionKey)
	}
	route = llmutil.SelectRouteCandidate(route, selectionKey)
	client, err := generation.commonDeps.CreateLLMClient(route)
	if err != nil {
		if closer, ok := client.(io.Closer); ok {
			return "", errors.Join(err, closer.Close())
		}
		return "", err
	}
	if closer, ok := client.(io.Closer); ok {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil && generation.logger != nil {
				generation.logger.Warn("console_topic_title_client_close_failed", "error", closeErr.Error())
			}
		}()
	}
	model := strings.TrimSpace(route.ClientConfig.Model)
	if model == "" {
		_, model = defaultLLMConfigForGeneration(generation)
	}
	task = textutil.TruncateRunes(strings.Join(strings.Fields(task), " "), 1200)
	finalOutput = textutil.TruncateRunes(strings.Join(strings.Fields(finalOutput), " "), 1200)
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
	title := textutil.TruncateRunes(task, consoleTopicTitleMaxChars)
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
	raw = textutil.TruncateRunes(raw, consoleTopicTitleMaxChars)
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
		Source:  strings.TrimSpace(in.Source),
		Event:   strings.TrimSpace(in.Event),
		Ref:     strings.TrimSpace(in.Ref),
		TraceID: strings.TrimSpace(in.TraceID),
	}
	if strings.TrimSpace(trigger.Source) == "" &&
		strings.TrimSpace(trigger.Event) == "" &&
		strings.TrimSpace(trigger.Ref) == "" &&
		strings.TrimSpace(trigger.TraceID) == "" {
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
	if strings.TrimSpace(trigger.TraceID) == "" {
		trigger.TraceID = fallback.TraceID
	}
	return trigger
}

func consoleTriggerSource(trigger daemonruntime.TaskTrigger) string {
	if source := strings.TrimSpace(trigger.Source); source != "" {
		return source
	}
	return "console"
}

func (r *consoleLocalRuntime) reloadAwarenessLoop() {
	if r == nil {
		return
	}
	r.stopAwarenessLoop()
	r.awarenessMu.Lock()
	workersCtx := r.workersCtx
	r.awarenessMu.Unlock()
	if workersCtx == nil {
		return
	}
	generation, err := r.captureGeneration()
	if err != nil {
		return
	}
	if !canSubmitGeneration(generation) {
		generation.release()
		return
	}
	reader := generation.reader
	hbCfg := channelopts.HeartbeatConfigFromReader(generation.reader)
	cronCfg := channelopts.CronConfigFromReader(generation.reader)
	logger := generation.logger
	if logger == nil {
		logger = slog.Default()
	}
	hbCtx, cancel := context.WithCancel(workersCtx)
	done := make(chan struct{})
	pokeRequests := make(chan awarenessloop.PokeRequest)
	var cronRequests chan awarenessloop.CronRequest
	if cronCfg.Enabled {
		cronRequests = make(chan awarenessloop.CronRequest)
	}
	r.awarenessMu.Lock()
	r.awarenessCancel = cancel
	r.awarenessDone = done
	r.awarenessPokeRequests = pokeRequests
	r.awarenessCronRequests = cronRequests
	r.awarenessMu.Unlock()

	go func() {
		defer func() {
			// Keep awarenessDone installed until both the loop and its generation
			// ownership have finished. Reload relies on this channel as the join
			// boundary before starting the next generation.
			generation.release()
			r.awarenessMu.Lock()
			if r.awarenessPokeRequests == pokeRequests {
				r.awarenessPokeRequests = nil
				r.awarenessCronRequests = nil
				r.awarenessCancel = nil
				r.awarenessDone = nil
			}
			close(done)
			r.awarenessMu.Unlock()
		}()
		if err := awarenessloop.Run(hbCtx, generation.commonDeps, awarenessloop.RunOptions{
			Interval:                hbCfg.Interval,
			TaskTimeout:             consoleDefaultTimeoutFromReader(reader),
			AgentLimits:             consoleAgentLimitsFromReader(reader),
			EngineToolsConfig:       consoleEngineToolsConfigFromReader(reader),
			Source:                  "console",
			ChecklistPath:           generation.paths.HeartbeatPath,
			DisableHeartbeat:        !hbCfg.Enabled || hbCfg.Interval <= 0,
			MemoryEnabled:           reader.GetBool("memory.enabled"),
			MemoryShortTermDays:     reader.GetInt("memory.short_term_days"),
			MemoryInjectionEnabled:  reader.GetBool("memory.injection.enabled"),
			MemoryInjectionMaxItems: reader.GetInt("memory.injection.max_items"),
			PokeRequests:            pokeRequests,
			CronRequests:            cronRequests,
			CronEnabled:             cronCfg.Enabled,
			CronPath:                generation.paths.CronPath,
			ChatInfoRefresher:       chatinfo.NewFetcher(chatinfo.FetcherOptionsFromReader(reader)),
			TaskStore:               r.store,
			CronNotify: func(_ context.Context, notification awarenessloop.CronNotification) error {
				r.notificationHub.Publish(notification)
				return nil
			},
		}); err != nil && hbCtx.Err() == nil {
			logger.Warn("console_awareness_error", "error", err.Error())
		}
	}()
}

func (r *consoleLocalRuntime) stopAwarenessLoop() {
	if r == nil {
		return
	}
	r.awarenessMu.Lock()
	cancel := r.awarenessCancel
	done := r.awarenessDone
	r.awarenessCancel = nil
	r.awarenessDone = nil
	r.awarenessPokeRequests = nil
	r.awarenessCronRequests = nil
	r.awarenessMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
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
