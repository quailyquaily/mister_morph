package agent

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/secrets"
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

func WithSkillAuthProfiles(authProfiles []string, enforce bool) Option {
	return func(e *Engine) {
		e.skillAuthProfiles = append([]string{}, authProfiles...)
		e.enforceSkillAuth = enforce
	}
}

type Config struct {
	MaxSteps         int
	MaxTokenBudget   int
	ParseRetries     int
	IntentEnabled    bool
	IntentTimeout    time.Duration
	IntentMaxHistory int
}

type Engine struct {
	client   llm.Client
	registry *tools.Registry
	config   Config
	spec     PromptSpec
	hooks    []Hook
	log      *slog.Logger
	logOpts  LogOptions

	promptBuilder    func(registry *tools.Registry, task string) string
	paramsBuilder    func(opts RunOptions) map[string]any
	onToolSuccess    func(ctx *Context, toolName string)
	onPlanStepUpdate func(ctx *Context, update PlanStepUpdate)
	fallbackFinal    func() *Final

	skillAuthProfiles []string
	enforceSkillAuth  bool

	guard *guard.Guard
}

func New(client llm.Client, registry *tools.Registry, cfg Config, spec PromptSpec, opts ...Option) *Engine {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 15
	}
	if cfg.ParseRetries < 0 {
		cfg.ParseRetries = 0
	}
	if cfg.IntentTimeout <= 0 {
		cfg.IntentTimeout = 8 * time.Second
	}
	if cfg.IntentMaxHistory <= 0 {
		cfg.IntentMaxHistory = 8
	}
	if spec.Identity == "" {
		spec = DefaultPromptSpec()
	}
	e := &Engine{
		client:   client,
		registry: registry,
		config:   cfg,
		spec:     spec,
		log:      slog.Default(),
		logOpts:  DefaultLogOptions(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

func (e *Engine) Run(ctx context.Context, task string, opts RunOptions) (*Final, *Context, error) {
	agentCtx := NewContext(task, e.config.MaxSteps)
	ctx = secrets.WithSkillAuthProfilePolicy(ctx, e.skillAuthProfiles, e.enforceSkillAuth)

	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}

	runID := newRunID()
	log := e.log.With("run_id", runID, "model", model)
	log.Info("run_start", "task_len", len(task))

	var intent Intent
	hasIntent := false
	if e.config.IntentEnabled && !isHeartbeatMeta(opts.Meta) {
		intentCtx := ctx
		var cancel context.CancelFunc
		if e.config.IntentTimeout > 0 {
			intentCtx, cancel = context.WithTimeout(ctx, e.config.IntentTimeout)
		}
		if cancel != nil {
			defer cancel()
		}
		inferred, err := InferIntent(intentCtx, e.client, model, task, opts.History, e.config.IntentMaxHistory)
		if err != nil {
			log.Warn("intent_infer_error", "error", err.Error())
		} else if !inferred.Empty() {
			intent = inferred
			hasIntent = true
			log.Debug("intent_inferred",
				"goal", truncateString(intent.Goal, 120),
				"deliverable", truncateString(intent.Deliverable, 120),
				"ask", intent.Ask,
			)
		}
	}

	var systemPrompt string
	if e.promptBuilder != nil {
		systemPrompt = e.promptBuilder(e.registry, task)
	} else {
		spec := augmentPromptSpecForTask(e.spec, task)
		if hasIntent {
			spec.Blocks = append(spec.Blocks, IntentBlock(intent))
		}
		systemPrompt = BuildSystemPrompt(e.registry, spec)
	}

	messages := []llm.Message{{Role: "system", Content: systemPrompt}}
	if hasIntent && e.promptBuilder != nil {
		messages = append(messages, llm.Message{Role: "system", Content: IntentSystemMessage(intent)})
	}
	for _, m := range opts.History {
		if strings.TrimSpace(strings.ToLower(m.Role)) == "system" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		messages = append(messages, m)
	}

	if metaMsg, ok := buildInjectedMetaMessage(opts.Meta); ok {
		messages = append(messages, llm.Message{Role: "user", Content: metaMsg})
		log.Debug("run_meta_injected", "meta_bytes", len(metaMsg))
	}

	messages = append(messages, llm.Message{Role: "user", Content: task})

	requestedWrites := ExtractFileWritePaths(task)

	planRequired := false

	var extraParams map[string]any
	if e.paramsBuilder != nil {
		extraParams = e.paramsBuilder(opts)
	}

	return e.runLoop(ctx, &engineLoopState{
		runID:           runID,
		model:           model,
		log:             log,
		messages:        messages,
		agentCtx:        agentCtx,
		extraParams:     extraParams,
		tools:           buildLLMTools(e.registry),
		planRequired:    planRequired,
		requestedWrites: requestedWrites,
		nextStep:        0,
	})
}

func (e *Engine) loadedSkillNames() map[string]bool {
	out := make(map[string]bool)
	spec := e.spec
	if len(spec.Blocks) == 0 {
		return out
	}
	for _, blk := range spec.Blocks {
		title := strings.TrimSpace(blk.Title)
		if title == "" {
			continue
		}
		name := title
		id := ""
		if i := strings.LastIndexByte(title, '('); i >= 0 && strings.HasSuffix(title, ")") {
			name = strings.TrimSpace(title[:i])
			id = strings.TrimSpace(strings.TrimSuffix(title[i+1:], ")"))
		}
		if strings.TrimSpace(name) != "" {
			out[strings.ToLower(name)] = true
		}
		if strings.TrimSpace(id) != "" {
			out[strings.ToLower(id)] = true
		}
	}
	return out
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

func isHeartbeatMeta(meta map[string]any) bool {
	if len(meta) == 0 {
		return false
	}
	if v, ok := meta["trigger"]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(strings.ToLower(s)) == "heartbeat" {
			return true
		}
	}
	if _, ok := meta["heartbeat"]; ok {
		return true
	}
	return false
}
