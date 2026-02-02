package agent

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/llm"
)

// maxObservationChars is the maximum length of a tool observation kept in the
// message history sent to the LLM. Longer observations are truncated to avoid
// overflowing the context window on long-running multi-step runs.
const maxObservationChars = 128 * 1024 // 128 KB

type engineLoopState struct {
	runID string
	model string
	log   *slog.Logger

	messages        []llm.Message
	agentCtx        *Context
	extraParams     map[string]any
	planRequired    bool
	parseFailures   int
	requestedWrites []string

	pendingTool         *pendingToolSnapshot
	approvedPendingTool bool

	nextStep int
}

func newRunID() string { return fmt.Sprintf("%x", rand.Uint64()) }

func (e *Engine) runLoop(ctx context.Context, st *engineLoopState) (*Final, *Context, error) {
	if st == nil || st.agentCtx == nil {
		return nil, nil, fmt.Errorf("nil engine state")
	}
	log := st.log
	if log == nil {
		log = slog.Default()
	}

	for step := st.nextStep; step < st.agentCtx.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			log.Warn("run_cancelled", "step", step, "error", err.Error())
			return nil, st.agentCtx, fmt.Errorf("context cancelled at step %d: %w", step, err)
		}

		for _, hook := range e.hooks {
			if err := hook(ctx, step, st.agentCtx, &st.messages); err != nil {
				log.Warn("hook_error", "step", step, "error", err.Error())
				return nil, st.agentCtx, err
			}
		}

		var (
			result llm.Result
			resp   AgentResponse
			err    error
		)

		if st.pendingTool != nil {
			resp = AgentResponse{Type: TypeToolCall, ToolCall: &st.pendingTool.ToolCall, RawFinalAnswer: nil}
			result = llm.Result{Text: st.pendingTool.AssistantText}
		} else {
			start := time.Now()
			log.Debug("llm_call_start", "step", step, "messages", len(st.messages))
			result, err = e.client.Chat(ctx, llm.Request{
				Model:      st.model,
				Messages:   st.messages,
				ForceJSON:  true,
				Parameters: st.extraParams,
			})
			if err != nil {
				log.Error("llm_call_error", "step", step, "error", err.Error())
				return nil, st.agentCtx, fmt.Errorf("LLM call failed at step %d: %w", step, err)
			}
			st.agentCtx.AddUsage(result.Usage, time.Since(start))
			log.Debug("llm_call_done",
				"step", step,
				"duration_ms", time.Since(start).Milliseconds(),
				"total_tokens", st.agentCtx.Metrics.TotalTokens,
			)

			if e.config.MaxTokenBudget > 0 && st.agentCtx.Metrics.TotalTokens > e.config.MaxTokenBudget {
				log.Warn("token_budget_exceeded", "step", step, "total_tokens", st.agentCtx.Metrics.TotalTokens, "budget", e.config.MaxTokenBudget)
				break
			}

			parsed, parseErr := ParseResponse(result)
			if parseErr != nil {
				st.parseFailures++
				st.agentCtx.Metrics.ParseRetries = st.parseFailures
				log.Warn("parse_error", "step", step, "retries", st.parseFailures, "error", parseErr.Error())
				if st.parseFailures > e.config.ParseRetries {
					break
				}
				st.messages = append(st.messages,
					llm.Message{Role: "assistant", Content: result.Text},
					llm.Message{Role: "user", Content: "Your response was not valid JSON. You MUST respond with a JSON object containing \"type\" as \"plan\", \"tool_call\", or \"final\". Try again."},
				)
				continue
			}
			st.parseFailures = 0
			resp = *parsed

			if st.planRequired && st.agentCtx.Plan == nil && resp.Type != TypePlan {
				log.Warn("plan_missing", "step", step, "got_type", resp.Type)
				st.messages = append(st.messages,
					llm.Message{Role: "assistant", Content: result.Text},
					llm.Message{Role: "user", Content: "You MUST respond with a plan first (type=\"plan\"). Do not call tools yet. Try again."},
				)
				continue
			}
		}

		switch resp.Type {
		case TypePlan:
			p := resp.PlanPayload()
			st.agentCtx.Plan = p
			NormalizePlanSteps(st.agentCtx.Plan)
			log.Info("plan", "step", step, "summary_len", len(strings.TrimSpace(p.Summary)), "steps", len(p.Steps))
			if e.logOpts.IncludeThoughts {
				thought := truncateString(p.Thought, e.logOpts.MaxThoughtChars)
				log.Info("plan_thought", "step", step, "thought", thought)
			} else {
				log.Debug("plan_thought_len", "step", step, "thought_len", len(p.Thought))
			}
			st.messages = append(st.messages,
				llm.Message{Role: "assistant", Content: result.Text},
				llm.Message{Role: "user", Content: "Plan received. Proceed to execute it. Use tools as needed, then return final."},
			)
			continue

		case TypeFinal, TypeFinalAnswer:
			st.agentCtx.RawFinalAnswer = resp.RawFinalAnswer
			fp := resp.FinalPayload()
			if fp != nil {
				if st.agentCtx.Plan != nil && fp.Plan == nil {
					fp.Plan = st.agentCtx.Plan
				}
				if st.agentCtx.Plan != nil {
					for i := range st.agentCtx.Plan.Steps {
						if st.agentCtx.Plan.Steps[i].Status != PlanStatusCompleted {
							log.Info("plan_step_completed", "step", step, "plan_step_index", i, "plan_step", st.agentCtx.Plan.Steps[i].Step, "reason", "final")
						}
					}
					CompleteAllPlanSteps(st.agentCtx.Plan)
				}

				if len(st.requestedWrites) > 0 {
					missing := missingFiles(st.requestedWrites)
					if len(missing) > 0 {
						if _, ok := e.registry.Get("write_file"); ok {
							log.Info("file_write_required", "step", step, "paths", strings.Join(missing, ", "))
							st.messages = append(st.messages,
								llm.Message{Role: "assistant", Content: result.Text},
								llm.Message{Role: "user", Content: fmt.Sprintf("You must write the requested file(s) before finishing: %s. Next, respond with a tool_call using write_file (preferred) or bash to create/update them. The file content should be the final markdown/report (do not include meta text like 'Writing to ...').", strings.Join(missing, ", "))},
							)
							continue
						}
						if _, ok := e.registry.Get("bash"); ok {
							log.Info("file_write_required", "step", step, "paths", strings.Join(missing, ", "))
							st.messages = append(st.messages,
								llm.Message{Role: "assistant", Content: result.Text},
								llm.Message{Role: "user", Content: fmt.Sprintf("You must write the requested file(s) before finishing: %s. Next, respond with a tool_call using bash to create/update them. The file content should be the final markdown/report (do not include meta text like 'Writing to ...').", strings.Join(missing, ", "))},
							)
							continue
						}
						log.Warn("file_write_unavailable", "paths", strings.Join(missing, ", "))
					}
				}

				// OutputPublish guard hook (redact-only).
				if e.guard != nil && e.guard.Enabled() {
					if s, ok := fp.Output.(string); ok && strings.TrimSpace(s) != "" {
						gr, _ := e.guard.Evaluate(ctx, guard.Meta{RunID: st.runID, Step: step, Time: time.Now().UTC()}, guard.Action{
							Type:    guard.ActionOutputPublish,
							Content: s,
						})
						if gr.Decision == guard.DecisionAllowWithRedact && strings.TrimSpace(gr.RedactedContent) != "" {
							fp.Output = gr.RedactedContent
						}
					}
				}

				thought := truncateString(fp.Thought, e.logOpts.MaxThoughtChars)
				if e.logOpts.IncludeThoughts {
					log.Info("final", "step", step, "thought", thought)
				} else {
					log.Info("final", "step", step, "thought_len", len(fp.Thought))
				}
			}
			return fp, st.agentCtx, nil

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
			} else {
				log.Debug("tool_thought_len", "step", step, "tool", tc.Name, "thought_len", len(tc.Thought))
			}

			observation, toolErr, pausedFinal, paused := e.executeToolWithGuard(ctx, st, step, result.Text, tc, stepStart)
			if paused {
				return pausedFinal, st.agentCtx, nil
			}

			st.agentCtx.RecordStep(Step{
				StepNumber:  step,
				Thought:     tc.Thought,
				Action:      tc.Name,
				ActionInput: tc.Params,
				Observation: observation,
				Error:       toolErr,
				Duration:    time.Since(stepStart),
			})

			if toolErr == nil && e.onToolSuccess != nil {
				e.onToolSuccess(st.agentCtx, tc.Name)
			}

			if toolErr == nil && st.agentCtx.Plan != nil {
				completedIdx, completedStep, startedIdx, startedStep, ok := AdvancePlanOnSuccess(st.agentCtx.Plan)
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

			msgObservation := observation
			if len(msgObservation) > maxObservationChars {
				msgObservation = msgObservation[:maxObservationChars] + "\n...(truncated)"
			}
			st.messages = append(st.messages,
				llm.Message{Role: "assistant", Content: result.Text},
				llm.Message{Role: "user", Content: fmt.Sprintf("Tool Result (%s):\n%s", tc.Name, msgObservation)},
			)

			// If this step came from a stored pending tool call, clear it and move on.
			st.pendingTool = nil
			st.approvedPendingTool = false
		default:
			log.Error("unexpected_response_type", "step", step, "type", resp.Type)
			return nil, st.agentCtx, ErrParseFailure
		}
	}

	return e.forceConclusion(ctx, st.messages, st.model, st.agentCtx, st.extraParams, log)
}

func (e *Engine) executeToolWithGuard(ctx context.Context, st *engineLoopState, step int, assistantText string, tc *ToolCall, stepStart time.Time) (string, error, *Final, bool) {
	var observation string
	var toolErr error

	tool, found := e.registry.Get(tc.Name)
	if !found {
		observation = fmt.Sprintf("Error: tool '%s' not found. Available tools: %s", tc.Name, e.registry.ToolNames())
		return observation, fmt.Errorf("tool not found"), nil, false
	}

	// Guard pre-tool decision.
	if e.guard != nil && e.guard.Enabled() {
		gr, _ := e.guard.Evaluate(ctx, guard.Meta{RunID: st.runID, Step: step, Time: time.Now().UTC()}, guard.Action{
			Type:       guard.ActionToolCallPre,
			ToolName:   tc.Name,
			ToolParams: tc.Params,
		})
		switch gr.Decision {
		case guard.DecisionDeny:
			observation = fmt.Sprintf("Error: blocked by guard (%s)", strings.Join(gr.Reasons, "; "))
			return observation, fmt.Errorf("blocked by guard"), nil, false
		case guard.DecisionRequireApproval:
			if st.approvedPendingTool {
				// Already approved; proceed.
				break
			}
			// Pause run and return a pending final.
			rs := resumeStateV1{
				RunID:             st.runID,
				Model:             st.model,
				Step:              step,
				PlanRequired:      st.planRequired,
				ParseFailures:     st.parseFailures,
				SkillAuthProfiles: append([]string{}, e.skillAuthProfiles...),
				EnforceSkillAuth:  e.enforceSkillAuth,
				Messages:          st.messages,
				ExtraParams:       st.extraParams,
				AgentCtx:          snapshotFromContext(st.agentCtx),
				PendingTool: pendingToolSnapshot{
					AssistantText: assistantText,
					ToolCall:      *tc,
				},
			}
			b, err := marshalResumeState(rs)
			if err != nil {
				return "", err, nil, false
			}
			sum := fmt.Sprintf("ToolCallPre tool=%s", tc.Name)
			id, err := e.guard.RequestApproval(ctx, guard.Meta{RunID: st.runID, Step: step, Time: time.Now().UTC()}, guard.Action{
				Type:       guard.ActionToolCallPre,
				ToolName:   tc.Name,
				ToolParams: tc.Params,
			}, gr, sum, b)
			if err != nil {
				observation = fmt.Sprintf("Error: approval request failed: %s", err.Error())
				return observation, err, nil, false
			}
			final := &Final{
				Output: PendingOutput{
					Status:            "pending",
					ApprovalRequestID: id,
					Message:           fmt.Sprintf("Approval required to execute tool %q at step %d.", tc.Name, step),
				},
				Plan: st.agentCtx.Plan,
			}
			return "", nil, final, true
		}
	}

	toolCtx := ctx
	if e.guard != nil && e.guard.Enabled() && strings.EqualFold(tc.Name, "url_fetch") {
		// Only enforce guard-level URL allowlists for unauthenticated url_fetch calls.
		authProfile, _ := tc.Params["auth_profile"].(string)
		if strings.TrimSpace(authProfile) == "" {
			if p, ok := e.guard.NetworkPolicyForURLFetch(); ok && len(p.AllowedURLPrefixes) > 0 {
				toolCtx = guard.WithNetworkPolicy(toolCtx, p)
			}
		}
	}

	observation, toolErr = tool.Execute(toolCtx, tc.Params)
	if toolErr != nil {
		if strings.TrimSpace(observation) == "" {
			observation = fmt.Sprintf("error: %s", toolErr.Error())
		} else {
			observation = fmt.Sprintf("%s\n\nerror: %s", observation, toolErr.Error())
		}
	}

	// Guard post-tool redaction (runs even when toolErr != nil).
	if e.guard != nil && e.guard.Enabled() {
		gr, _ := e.guard.Evaluate(ctx, guard.Meta{RunID: st.runID, Step: step, Time: time.Now().UTC()}, guard.Action{
			Type:     guard.ActionToolCallPost,
			ToolName: tc.Name,
			Content:  observation,
		})
		switch gr.Decision {
		case guard.DecisionAllowWithRedact:
			if strings.TrimSpace(gr.RedactedContent) != "" {
				observation = gr.RedactedContent
			}
		case guard.DecisionDeny:
			observation = "Error: blocked by guard (tool output)"
			if toolErr == nil {
				toolErr = fmt.Errorf("blocked by guard")
			}
		}
	}

	_ = stepStart
	return observation, toolErr, nil, false
}
