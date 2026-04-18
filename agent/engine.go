package agent

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/platformutil"
	"github.com/quailyquaily/mistermorph/internal/runtimeclock"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type Hook func(ctx context.Context, step int, agentCtx *Context, messages *[]llm.Message) error

type Option func(*Engine)

func WithHook(h Hook) Option {
	return func(e *Engine) {
		if h != nil {
			e.hooks = append(e.hooks, h)
		}
	}
}

func WithLogger(l *slog.Logger) Option {
	return func(e *Engine) {
		if l != nil {
			e.log = l
		}
	}
}

// WithPromptBuilder replaces the default system prompt builder.
// This hook is intended for tests in this repository.
func WithPromptBuilder(fn func(*tools.Registry, string) string) Option {
	return func(e *Engine) {
		if fn != nil {
			e.promptBuilder = fn
		}
	}
}

func WithParamsBuilder(fn func(RunOptions) map[string]any) Option {
	return func(e *Engine) {
		if fn != nil {
			e.paramsBuilder = fn
		}
	}
}

func WithOnToolStart(fn func(*Context, string)) Option {
	return func(e *Engine) {
		if fn != nil {
			e.onToolStart = fn
		}
	}
}

func WithOnToolSuccess(fn func(*Context, string)) Option {
	return func(e *Engine) {
		if fn != nil {
			e.onToolSuccess = fn
		}
	}
}

func WithPlanStepUpdate(fn func(*Context, PlanStepUpdate)) Option {
	return func(e *Engine) {
		if fn != nil {
			e.onPlanStepUpdate = fn
		}
	}
}

func WithFallbackFinal(fn func() *Final) Option {
	return func(e *Engine) {
		if fn != nil {
			e.fallbackFinal = fn
		}
	}
}

// SubClientFactory creates an LLM client for a sub-agent with the given prefix
// (used for inspection dump filenames). The returned cleanup function must be
// called after the sub-agent completes to close any resources (e.g. dump files).
type SubClientFactory func(prefix string) (client llm.Client, cleanup func())

func WithSubClientFactory(fn SubClientFactory) Option {
	return func(e *Engine) {
		if fn != nil {
			e.subClientFactory = fn
		}
	}
}

func WithSubtaskRunner(runner SubtaskRunner) Option {
	return func(e *Engine) {
		if runner != nil {
			e.subtaskRunner = runner
		}
	}
}

func WithEngineToolsConfig(cfg EngineToolsConfig) Option {
	return func(e *Engine) {
		e.engineToolsConfig = cfg
	}
}

func WithSpawnToolEnabled(enabled bool) Option {
	return func(e *Engine) {
		e.engineToolsConfig.SpawnEnabled = enabled
	}
}

func WithACPSpawnToolEnabled(enabled bool) Option {
	return func(e *Engine) {
		e.engineToolsConfig.ACPSpawnEnabled = enabled
	}
}

func WithACPAgents(configs []acpclient.AgentConfig) Option {
	return func(e *Engine) {
		e.acpAgents = acpclient.CloneAgents(configs)
	}
}

type Config struct {
	MaxSteps        int
	MaxTokenBudget  int
	ParseRetries    int
	ToolRepeatLimit int
	DefaultModel    string
	ToolCallTimeout time.Duration
}

type Engine struct {
	client   llm.Client
	registry *tools.Registry
	config   Config
	spec     PromptSpec
	hooks    []Hook
	log      *slog.Logger
	logOpts  LogOptions

	engineToolsConfig EngineToolsConfig

	promptBuilder    func(registry *tools.Registry, task string) string
	paramsBuilder    func(opts RunOptions) map[string]any
	onToolStart      func(ctx *Context, toolName string)
	onToolSuccess    func(ctx *Context, toolName string)
	onPlanStepUpdate func(ctx *Context, update PlanStepUpdate)
	fallbackFinal    func() *Final

	subClientFactory SubClientFactory
	subtaskRunner    SubtaskRunner
	acpAgents        []acpclient.AgentConfig

	guard *guard.Guard
}

func New(client llm.Client, registry *tools.Registry, cfg Config, spec PromptSpec, opts ...Option) *Engine {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 15
	}
	if cfg.ParseRetries < 0 {
		cfg.ParseRetries = 0
	}
	if cfg.ToolRepeatLimit <= 0 {
		cfg.ToolRepeatLimit = 3
	}
	if spec.Identity == "" {
		spec = DefaultPromptSpec()
	}
	e := &Engine{
		client:            client,
		registry:          registry,
		config:            cfg,
		spec:              spec,
		log:               slog.Default(),
		logOpts:           DefaultLogOptions(),
		engineToolsConfig: DefaultEngineToolsConfig(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	if e.subtaskRunner == nil {
		e.subtaskRunner = &localSubtaskRunner{engine: e}
	}
	registerEngineTools(
		e.registry,
		e.engineToolsConfig,
		spawnToolDeps{
			LookupTool:   e.registry.Get,
			DefaultModel: e.config.DefaultModel,
			Runner:       e.subtaskRunner,
		},
		acpSpawnToolDeps{
			LookupAgent: func(name string) (acpclient.AgentConfig, bool) {
				return acpclient.FindAgent(e.acpAgents, name)
			},
			Runner:    e.subtaskRunner,
			RunPrompt: acpclient.RunPrompt,
		},
	)
	return e
}

func (e *Engine) Run(ctx context.Context, task string, opts RunOptions) (*Final, *Context, error) {
	agentCtx := NewContext(task, e.config.MaxSteps)

	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = strings.TrimSpace(e.config.DefaultModel)
	}

	runID := llmstats.RunIDFromContext(ctx)
	if runID == "" {
		runID = newRunID()
	}
	ctx = llmstats.WithRunID(ctx, runID)
	log := e.log.With("run_id", runID, "model", model)
	log.Info("run_start", "task_len", len(task))

	var systemPrompt string
	if e.promptBuilder != nil {
		systemPrompt = e.promptBuilder(e.registry, task)
	} else {
		systemPrompt = BuildSystemPrompt(e.registry, e.spec)
	}

	messages := []llm.Message{{Role: "system", Content: systemPrompt}}

	injectedMeta := runtimeclock.WithRuntimeClockMeta(opts.Meta, time.Now())
	if _, ok := injectedMeta["host_os"]; !ok {
		injectedMeta["host_os"] = platformutil.Current()
	}
	if metaMsg, ok := buildInjectedMetaMessage(injectedMeta); ok {
		trigger := ""
		if v, ok := injectedMeta["trigger"].(string); ok {
			trigger = strings.TrimSpace(v)
		}
		messages = append(messages, llm.Message{Role: "user", Content: metaMsg})
		log.Debug(
			"run_meta_injected",
			"meta_bytes", len(metaMsg),
			"meta_keys", sortedMapKeys(injectedMeta),
			"meta_trigger", trigger,
			"meta_payload", metaMsg,
		)
	}

	if memoryMsg, ok := buildInjectedMemoryMessage(opts.MemoryContext); ok {
		messages = append(messages, llm.Message{Role: "user", Content: memoryMsg})
		log.Debug("run_memory_injected", "memory_bytes", len(memoryMsg))
	}

	for _, m := range opts.History {
		if strings.TrimSpace(strings.ToLower(m.Role)) == "system" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" && len(m.Parts) == 0 {
			continue
		}
		messages = append(messages, m)
	}

	if opts.CurrentMessage != nil {
		current := *opts.CurrentMessage
		current.Role = "user"
		if strings.TrimSpace(current.Content) != "" || len(current.Parts) > 0 {
			messages = append(messages, current)
		}
	} else if !opts.SkipTaskMessage {
		if strings.TrimSpace(task) != "" {
			messages = append(messages, llm.Message{Role: "user", Content: task})
		}
	}

	requestedWrites := ExtractFileWritePaths(task)

	planRequired := false

	var extraParams map[string]any
	if e.paramsBuilder != nil {
		extraParams = e.paramsBuilder(opts)
	}

	return e.runLoop(ctx, &engineLoopState{
		runID:           runID,
		model:           model,
		scene:           strings.TrimSpace(opts.Scene),
		log:             log,
		messages:        messages,
		agentCtx:        agentCtx,
		extraParams:     extraParams,
		tools:           buildLLMTools(e.registry),
		planRequired:    planRequired,
		requestedWrites: requestedWrites,
		onStream:        opts.OnStream,
		nextStep:        0,
	})
}

func missingFiles(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

func sortedMapKeys(m map[string]any) []string {
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
