package agent

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mister_morph/llm"
	"github.com/quailyquaily/mister_morph/tools"
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

func WithFallbackFinal(fn func() *Final) Option {
	return func(e *Engine) {
		if fn != nil {
			e.fallbackFinal = fn
		}
	}
}

type Config struct {
	MaxSteps       int
	MaxTokenBudget int
	ParseRetries   int
	PlanMode       string // off|auto|always
}

type Engine struct {
	client   llm.Client
	registry *tools.Registry
	config   Config
	spec     PromptSpec
	hooks    []Hook
	log      *slog.Logger
	logOpts  LogOptions

	promptBuilder func(registry *tools.Registry, task string) string
	paramsBuilder func(opts RunOptions) map[string]any
	onToolSuccess func(ctx *Context, toolName string)
	fallbackFinal func() *Final
}

func New(client llm.Client, registry *tools.Registry, cfg Config, spec PromptSpec, opts ...Option) *Engine {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 15
	}
	if cfg.ParseRetries < 0 {
		cfg.ParseRetries = 0
	}
	if strings.TrimSpace(cfg.PlanMode) == "" {
		cfg.PlanMode = "auto"
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

	model := opts.Model
	if model == "" {
		model = "gpt-4o-mini"
	}

	runID := fmt.Sprintf("%x", rand.Uint64())
	log := e.log.With(
		"run_id", runID,
		"model", model,
	)
	log.Info("run_start", "task_len", len(task))

	var systemPrompt string
	if e.promptBuilder != nil {
		systemPrompt = e.promptBuilder(e.registry, task)
	} else {
		systemPrompt = BuildSystemPrompt(e.registry, e.spec)
	}
	messages := []llm.Message{{Role: "system", Content: systemPrompt}}
	for _, m := range opts.History {
		// Avoid duplicating system prompts or unknown roles.
		if strings.TrimSpace(strings.ToLower(m.Role)) == "system" {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		messages = append(messages, m)
	}
	messages = append(messages, llm.Message{Role: "user", Content: task})

	requestedWrites := ExtractFileWritePaths(task)

	planMode := strings.ToLower(strings.TrimSpace(e.config.PlanMode))
	planRequired := false
	switch planMode {
	case "off", "none", "disabled", "false", "0":
		planRequired = false
	case "always", "on", "true", "1":
		planRequired = true
	case "auto", "":
		planRequired = TaskNeedsPlan(task)
	default:
		log.Warn("unknown_plan_mode", "mode", planMode)
		planRequired = TaskNeedsPlan(task)
	}
	if planRequired {
		log.Info("plan_required", "mode", planMode)
		messages = append(messages, llm.Message{
			Role: "user",
			Content: "Before calling any tools, return a concise plan as JSON with type=\"plan\". " +
				"Do NOT call tools in the plan response; you will execute the plan afterwards.",
		})
	}

	var extraParams map[string]any
	if e.paramsBuilder != nil {
		extraParams = e.paramsBuilder(opts)
	}

	parseFailures := 0
	for step := 0; step < agentCtx.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			log.Warn("run_cancelled", "step", step, "error", err.Error())
			return nil, agentCtx, fmt.Errorf("context cancelled at step %d: %w", step, err)
		}

		for _, hook := range e.hooks {
			if err := hook(ctx, step, agentCtx, &messages); err != nil {
				log.Warn("hook_error", "step", step, "error", err.Error())
				return nil, agentCtx, err
			}
		}

		start := time.Now()
		log.Debug("llm_call_start", "step", step, "messages", len(messages))
		result, err := e.client.Chat(ctx, llm.Request{
			Model:      model,
			Messages:   messages,
			ForceJSON:  true,
			Parameters: extraParams,
		})
		if err != nil {
			log.Error("llm_call_error", "step", step, "error", err.Error())
			return nil, agentCtx, fmt.Errorf("LLM call failed at step %d: %w", step, err)
		}
		agentCtx.AddUsage(result.Usage, time.Since(start))
		log.Debug("llm_call_done",
			"step", step,
			"duration_ms", time.Since(start).Milliseconds(),
			"total_tokens", agentCtx.Metrics.TotalTokens,
		)

		if e.config.MaxTokenBudget > 0 && agentCtx.Metrics.TotalTokens > e.config.MaxTokenBudget {
			log.Warn("token_budget_exceeded", "step", step, "total_tokens", agentCtx.Metrics.TotalTokens, "budget", e.config.MaxTokenBudget)
			break
		}

		resp, parseErr := ParseResponse(result)
		if parseErr != nil {
			parseFailures++
			agentCtx.Metrics.ParseRetries = parseFailures
			log.Warn("parse_error", "step", step, "retries", parseFailures, "error", parseErr.Error())
			if parseFailures > e.config.ParseRetries {
				break
			}
			messages = append(messages,
				llm.Message{Role: "assistant", Content: result.Text},
				llm.Message{Role: "user", Content: "Your response was not valid JSON. You MUST respond with a JSON object containing \"type\" as \"plan\", \"tool_call\", or \"final\". Try again."},
			)
			continue
		}
		parseFailures = 0

		if planRequired && agentCtx.Plan == nil && resp.Type != TypePlan {
			log.Warn("plan_missing", "step", step, "got_type", resp.Type)
			messages = append(messages,
				llm.Message{Role: "assistant", Content: result.Text},
				llm.Message{Role: "user", Content: "You MUST respond with a plan first (type=\"plan\"). Do not call tools yet. Try again."},
			)
			continue
		}

		switch resp.Type {
		case TypePlan:
			p := resp.PlanPayload()
			agentCtx.Plan = p
			NormalizePlanSteps(agentCtx.Plan)
			log.Info("plan", "step", step, "summary_len", len(strings.TrimSpace(p.Summary)), "steps", len(p.Steps))
			if e.logOpts.IncludeThoughts {
				thought := truncateString(p.Thought, e.logOpts.MaxThoughtChars)
				log.Info("plan_thought", "step", step, "thought", thought)
			} else {
				log.Debug("plan_thought_len", "step", step, "thought_len", len(p.Thought))
			}
			messages = append(messages,
				llm.Message{Role: "assistant", Content: result.Text},
				llm.Message{Role: "user", Content: "Plan received. Proceed to execute it. Use tools as needed, then return final."},
			)
			continue
		case TypeFinal, TypeFinalAnswer:
			agentCtx.RawFinalAnswer = resp.RawFinalAnswer
			if fp := resp.FinalPayload(); fp != nil {
				if agentCtx.Plan != nil && fp.Plan == nil {
					fp.Plan = agentCtx.Plan
				}

				if agentCtx.Plan != nil {
					for i := range agentCtx.Plan.Steps {
						if agentCtx.Plan.Steps[i].Status != PlanStatusCompleted {
							log.Info("plan_step_completed", "step", step, "plan_step_index", i, "plan_step", agentCtx.Plan.Steps[i].Step, "reason", "final")
						}
					}
					CompleteAllPlanSteps(agentCtx.Plan)
				}

				if len(requestedWrites) > 0 {
					missing := missingFiles(requestedWrites)
					if len(missing) > 0 {
						if _, ok := e.registry.Get("write_file"); ok {
							log.Info("file_write_required", "step", step, "paths", strings.Join(missing, ", "))
							messages = append(messages,
								llm.Message{Role: "assistant", Content: result.Text},
								llm.Message{Role: "user", Content: fmt.Sprintf("You must write the requested file(s) before finishing: %s. Next, respond with a tool_call using write_file (preferred) or bash to create/update them. The file content should be the final markdown/report (do not include meta text like 'Writing to ...').", strings.Join(missing, ", "))},
							)
							continue
						}
						if _, ok := e.registry.Get("bash"); ok {
							log.Info("file_write_required", "step", step, "paths", strings.Join(missing, ", "))
							messages = append(messages,
								llm.Message{Role: "assistant", Content: result.Text},
								llm.Message{Role: "user", Content: fmt.Sprintf("You must write the requested file(s) before finishing: %s. Next, respond with a tool_call using bash to create/update them. The file content should be the final markdown/report (do not include meta text like 'Writing to ...').", strings.Join(missing, ", "))},
							)
							continue
						}
						log.Warn("file_write_unavailable", "paths", strings.Join(missing, ", "))
					}
				}

				thought := truncateString(fp.Thought, e.logOpts.MaxThoughtChars)
				if e.logOpts.IncludeThoughts {
					log.Info("final", "step", step, "thought", thought)
				} else {
					log.Info("final", "step", step, "thought_len", len(fp.Thought))
				}
				if e.logOpts.IncludeThoughts {
					log.Debug("final_thought", "step", step, "thought", thought)
				} else {
					log.Debug("final_thought_len", "step", step, "thought_len", len(fp.Thought))
				}
			}
			return resp.FinalPayload(), agentCtx, nil
		case TypeToolCall:
			tc := resp.ToolCall
			stepStart := time.Now()

			log.Info("tool_call", "step", step, "tool", tc.Name, "args", toolArgsSummary(tc.Name, tc.Params, e.logOpts))
			if log.Enabled(ctx, slog.LevelDebug) {
				fields := []any{"step", step, "tool", tc.Name, "param_keys", sortedMapKeys(tc.Params)}
				if e.logOpts.IncludeToolParams {
					fields = append(fields, "params", paramsAsJSON(tc.Params, e.logOpts.MaxJSONBytes, e.logOpts.MaxStringValueChars, e.logOpts.RedactKeys))
				}
				log.Debug("tool_call_params", fields...)
			}
			if e.logOpts.IncludeToolParams {
				log.Info("tool_call_params", "step", step, "tool", tc.Name,
					"params", paramsAsJSON(tc.Params, e.logOpts.MaxJSONBytes, e.logOpts.MaxStringValueChars, e.logOpts.RedactKeys),
				)
			}
			thought := truncateString(tc.Thought, e.logOpts.MaxThoughtChars)
			if e.logOpts.IncludeThoughts {
				log.Info("tool_thought", "step", step, "tool", tc.Name, "thought", thought)
			}
			if e.logOpts.IncludeThoughts {
				log.Debug("tool_thought", "step", step, "tool", tc.Name, "thought", thought)
			} else {
				log.Debug("tool_thought_len", "step", step, "tool", tc.Name, "thought_len", len(tc.Thought))
			}

			var observation string
			var toolErr error
			tool, found := e.registry.Get(tc.Name)
			if !found {
				observation = fmt.Sprintf("Error: tool '%s' not found. Available tools: %s", tc.Name, e.registry.ToolNames())
			} else {
				observation, toolErr = tool.Execute(ctx, tc.Params)
				if toolErr != nil {
					if strings.TrimSpace(observation) == "" {
						observation = fmt.Sprintf("error: %s", toolErr.Error())
					} else {
						observation = fmt.Sprintf("%s\n\nerror: %s", observation, toolErr.Error())
					}
				}
			}

			agentCtx.RecordStep(Step{
				StepNumber:  step,
				Thought:     tc.Thought,
				Action:      tc.Name,
				ActionInput: tc.Params,
				Observation: observation,
				Error:       toolErr,
				Duration:    time.Since(stepStart),
			})

			if found && toolErr == nil && e.onToolSuccess != nil {
				e.onToolSuccess(agentCtx, tc.Name)
			}

			if toolErr == nil && agentCtx.Plan != nil {
				completedIdx, completedStep, startedIdx, startedStep, ok := AdvancePlanOnSuccess(agentCtx.Plan)
				if ok {
					fields := []any{
						"step", step,
						"tool", tc.Name,
						"plan_step_index", completedIdx,
						"plan_step", completedStep,
					}
					if startedIdx != -1 && strings.TrimSpace(startedStep) != "" {
						fields = append(fields,
							"next_plan_step_index", startedIdx,
							"next_plan_step", startedStep,
						)
					}
					log.Info("plan_step_completed", fields...)
				}
			}

			if toolErr != nil {
				log.Warn("tool_done",
					"step", step,
					"tool", tc.Name,
					"duration_ms", time.Since(stepStart).Milliseconds(),
					"observation_len", len(observation),
					"error", toolErr.Error(),
				)
			} else {
				log.Info("tool_done",
					"step", step,
					"tool", tc.Name,
					"duration_ms", time.Since(stepStart).Milliseconds(),
					"observation_len", len(observation),
				)
			}

			messages = append(messages,
				llm.Message{Role: "assistant", Content: result.Text},
				llm.Message{Role: "user", Content: fmt.Sprintf("Tool Result (%s):\n%s", tc.Name, observation)},
			)
		default:
			log.Error("unexpected_response_type", "step", step, "type", resp.Type)
			return nil, agentCtx, ErrParseFailure
		}
	}

	return e.forceConclusion(ctx, messages, model, agentCtx, extraParams, log)
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

func (e *Engine) forceConclusion(ctx context.Context, messages []llm.Message, model string, agentCtx *Context, extraParams map[string]any, log *slog.Logger) (*Final, *Context, error) {
	if log == nil {
		log = e.log.With("model", model)
	}
	log.Warn("force_conclusion", "steps", len(agentCtx.Steps), "messages", len(messages))
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: "You have reached the maximum number of steps or token budget. Provide your final output NOW as a JSON final response.",
	})

	result, err := e.client.Chat(ctx, llm.Request{
		Model:      model,
		Messages:   messages,
		ForceJSON:  true,
		Parameters: extraParams,
	})
	if err != nil {
		log.Error("force_conclusion_llm_error", "error", err.Error())
		if e.fallbackFinal != nil {
			return e.fallbackFinal(), agentCtx, nil
		}
		return &Final{Output: "insufficient_evidence", Plan: agentCtx.Plan}, agentCtx, nil
	}
	agentCtx.AddUsage(result.Usage, result.Duration)

	resp, err := ParseResponse(result)
	if err != nil {
		log.Warn("force_conclusion_parse_error", "error", err.Error())
		if e.fallbackFinal != nil {
			return e.fallbackFinal(), agentCtx, nil
		}
		return &Final{Output: "insufficient_evidence", Plan: agentCtx.Plan}, agentCtx, nil
	}
	if resp.Type != TypeFinal && resp.Type != TypeFinalAnswer {
		log.Warn("force_conclusion_invalid_type", "type", resp.Type)
		if e.fallbackFinal != nil {
			return e.fallbackFinal(), agentCtx, nil
		}
		return &Final{Output: "insufficient_evidence", Plan: agentCtx.Plan}, agentCtx, nil
	}
	agentCtx.RawFinalAnswer = resp.RawFinalAnswer
	log.Info("force_conclusion_final")
	fp := resp.FinalPayload()
	if agentCtx.Plan != nil && fp != nil && fp.Plan == nil {
		fp.Plan = agentCtx.Plan
	}
	return fp, agentCtx, nil
}

func sortedMapKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// small input; insertion sort is fine but use stdlib.
	// keep deterministic for logs.
	sort.Strings(out)
	return out
}

func toolArgsSummary(toolName string, params map[string]any, opts LogOptions) map[string]any {
	if len(params) == 0 {
		return nil
	}

	out := make(map[string]any)
	switch toolName {
	case "url_fetch":
		if v, ok := params["url"].(string); ok && strings.TrimSpace(v) != "" {
			out["url"] = sanitizeURLForLog(v, opts)
		}
	case "web_search":
		if v, ok := params["q"].(string); ok && strings.TrimSpace(v) != "" {
			out["q"] = truncateString(strings.TrimSpace(v), opts.MaxStringValueChars)
		}
	case "read_file":
		if v, ok := params["path"].(string); ok && strings.TrimSpace(v) != "" {
			out["path"] = truncateString(strings.TrimSpace(v), opts.MaxStringValueChars)
		}
	case "echo":
		if v, ok := params["value"].(string); ok && strings.TrimSpace(v) != "" {
			out["value"] = truncateString(strings.TrimSpace(v), 200)
		}
	case "bash":
		// Bash commands may contain secrets; only log command when explicitly enabled.
		if opts.IncludeToolParams {
			if v, ok := params["cmd"].(string); ok && strings.TrimSpace(v) != "" {
				out["cmd"] = truncateString(strings.TrimSpace(v), 500)
			}
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
