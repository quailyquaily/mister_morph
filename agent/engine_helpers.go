package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
)

const (
	forceConclusionFallbackOutputTemplate = "I made it through %d steps, then got stuck while wrapping up: %s. pfft pfft pfft, pff pff pff."
	forceConclusionReasonModelCallFailed  = "the model request ffffffailed, wwwtttfffff."
	forceConclusionReasonFinalFormat      = "the final answer format was iiiinvalid. cooommonn, you can do itttt."
	forceConclusionReasonTypeTemplate     = "the model returned %q instead of a final answer, wwwtttfffff."
)

func buildForceConclusionFallbackOutput(stepCount int, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "an unknown issue came up"
	}
	return fmt.Sprintf(forceConclusionFallbackOutputTemplate, stepCount, reason)
}

func summarizeForceConclusionModelError(err error) string {
	if err == nil {
		return forceConclusionReasonModelCallFailed
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return "the model request timed out"
	case strings.Contains(msg, "rate limit"), strings.Contains(msg, "too many requests"), strings.Contains(msg, "429"):
		return "the model request was rate-limited"
	case strings.Contains(msg, "network"), strings.Contains(msg, "connection"), strings.Contains(msg, "dial"), strings.Contains(msg, "refused"), strings.Contains(msg, "reset"):
		return "there was a network issue reaching the model"
	default:
		return forceConclusionReasonModelCallFailed
	}
}

type forceConclusionReason string

const (
	forceConclusionMaxSteps     forceConclusionReason = "max_steps"
	forceConclusionTokenBudget  forceConclusionReason = "token_budget"
	forceConclusionParseRetries forceConclusionReason = "parse_retries_exhausted"
	forceConclusionTaskDeadline forceConclusionReason = "task_deadline_exceeded"
)

func (e *Engine) forceConclusion(ctx context.Context, st *engineLoopState, reason forceConclusionReason, log *slog.Logger) (*Final, *Context, error) {
	if st == nil || st.agentCtx == nil {
		return nil, nil, fmt.Errorf("nil engine state")
	}
	agentCtx := st.agentCtx
	deadlineReached := errors.Is(ctx.Err(), context.DeadlineExceeded)
	if ctx.Err() != nil && !deadlineReached {
		return nil, agentCtx, ctx.Err()
	}
	if deadlineReached {
		// A child must stop with its parent, not start an independent conclusion.
		if SubtaskDepthFromContext(ctx) > 0 {
			return nil, agentCtx, ctx.Err()
		}
		// The task may expire between reaching a limit and starting its summary.
		reason = forceConclusionTaskDeadline
		st.deadlineConclusion = true
		// Providers enforce the selected profile's request_timeout per attempt.
		// Detach the expired task deadline without adding a separate summary limit.
		ctx = context.WithoutCancel(ctx)
	}
	if log == nil {
		log = e.log.With("model", st.model)
	}
	steps := len(agentCtx.Steps)
	log.Warn("force_conclusion", "reason", reason, "steps", steps, "messages", len(st.messages))
	var prompt string
	switch reason {
	case forceConclusionMaxSteps:
		prompt = "You have reached the maximum number of steps."
	case forceConclusionTokenBudget:
		prompt = "You have exceeded the token budget."
	case forceConclusionParseRetries:
		prompt = "Your responses could not be parsed and the response-format retry limit has been exhausted."
	case forceConclusionTaskDeadline:
		prompt = "The parent task deadline has expired. Do not run any more tools. Summarize the available results, explicitly identify unfinished or failed work, and do not claim that all work completed. Provide your partial final output NOW as a JSON final response."
	}
	if reason != forceConclusionTaskDeadline {
		prompt += " Do not run any more tools. Provide your final output NOW as a JSON final response."
	}
	if deadlineReached {
		if st.onStream != nil {
			// Reset any partial response from the expired request before summarizing.
			if err := st.onStream(llm.StreamEvent{Done: true}); err != nil {
				log.Warn("force_conclusion_stream_reset_error", "error", err.Error())
			}
		}
	}
	st.messages = append(st.messages, llm.Message{
		Role:    "user",
		Content: prompt,
	})
	st.protectLastMessage()
	finishFallback := func(reason string) (*Final, *Context, error) {
		var fallback *Final
		if deadlineReached {
			fallback = &Final{Output: deadlinePartialOutput(agentCtx), Plan: agentCtx.Plan}
		} else if e.fallbackFinal != nil {
			fallback = e.fallbackFinal()
		} else {
			fallback = &Final{Output: buildForceConclusionFallbackOutput(steps, reason), Plan: agentCtx.Plan}
		}
		fallback, err := e.finalEgress(ctx, st, agentCtx.MaxSteps, fallback, nil)
		return fallback, agentCtx, err
	}

	result, err := e.callMainWithContextCompaction(ctx, st, agentCtx.MaxSteps, nil)
	if err != nil {
		log.Error("force_conclusion_llm_error", "error", err.Error())
		if !deadlineReached && ctx.Err() != nil {
			return nil, agentCtx, ctx.Err()
		}
		return finishFallback(summarizeForceConclusionModelError(err))
	}
	agentCtx.AddUsage(result.Usage, result.Duration)

	resp, err := ParseResponse(result)
	if err != nil {
		log.Warn("force_conclusion_parse_error", "error", err.Error())
		return finishFallback(forceConclusionReasonFinalFormat)
	}
	if resp.Type != TypeFinal && resp.Type != TypeFinalAnswer {
		log.Warn("force_conclusion_invalid_type", "type", resp.Type)
		return finishFallback(fmt.Sprintf(forceConclusionReasonTypeTemplate, resp.Type))
	}
	log.Info("force_conclusion_final")
	fp := resp.FinalPayload()
	if agentCtx.Plan != nil && fp != nil && (fp.Plan == nil || deadlineReached) {
		fp.Plan = agentCtx.Plan
	}
	if deadlineReached && fp != nil && len(resp.RawFinalAnswer) > 0 {
		// Keep raw consumers consistent with the preserved partial plan, too.
		var payload map[string]any
		if err := json.Unmarshal(resp.RawFinalAnswer, &payload); err != nil {
			return finishFallback(forceConclusionReasonFinalFormat)
		}
		payload["plan"] = fp.Plan
		resp.RawFinalAnswer, err = json.Marshal(payload)
		if err != nil {
			return finishFallback(forceConclusionReasonFinalFormat)
		}
	}
	fp, err = e.finalEgress(ctx, st, agentCtx.MaxSteps, fp, resp.RawFinalAnswer)
	return fp, agentCtx, err
}

func deadlinePartialOutput(ctx *Context) string {
	var output strings.Builder
	output.WriteString("Task deadline reached. This is a partial result; unfinished work has not been confirmed as completed.")
	if len(ctx.Steps) == 0 {
		output.WriteString("\nNo tool results were recorded before the deadline.")
	}
	for _, step := range ctx.Steps {
		fmt.Fprintf(&output, "\n\n[%s] %s\n%s", toolEventStatus(step.Error), step.Action, truncateString(step.Observation, 2000))
	}
	return output.String()
}

func toolArgsSummary(toolName string, params map[string]any, opts LogOptions, debugMode bool) map[string]any {
	if len(params) == 0 {
		return nil
	}

	out := make(map[string]any)
	switch toolName {
	case "url_fetch":
		if v, ok := params["url"].(string); ok && strings.TrimSpace(v) != "" {
			out["url"] = sanitizeURLForLog(v, opts)
		}
		if debugMode {
			method := "GET"
			if v, ok := params["method"].(string); ok && strings.TrimSpace(v) != "" {
				method = strings.ToUpper(strings.TrimSpace(v))
			}
			out["method"] = method

			if headers, ok := params["headers"]; ok && headers != nil {
				if mapped, ok := headers.(map[string]string); ok {
					converted := make(map[string]any, len(mapped))
					for k, v := range mapped {
						converted[k] = v
					}
					headers = converted
				}
				out["headers"] = sanitizeValue(headers, opts.MaxStringValueChars, opts.RedactKeys, "")
			}

			if body, ok := params["body"]; ok {
				out["body"] = sanitizeValue(body, opts.MaxStringValueChars, opts.RedactKeys, "")
			}
		}
	case "web_search":
		if v, ok := params["q"].(string); ok && strings.TrimSpace(v) != "" {
			out["q"] = truncateString(strings.TrimSpace(v), opts.MaxStringValueChars)
		}
	case "read_file":
		if v, ok := params["path"].(string); ok && strings.TrimSpace(v) != "" {
			out["path"] = truncateString(strings.TrimSpace(v), opts.MaxStringValueChars)
		}
	case "contacts_send", "agent_send":
		if v, ok := params["contact_id"].(string); ok && strings.TrimSpace(v) != "" {
			out["contact_id"] = truncateString(strings.TrimSpace(v), opts.MaxStringValueChars)
		}
		if v, ok := params["content_type"].(string); ok && strings.TrimSpace(v) != "" {
			out["content_type"] = truncateString(strings.TrimSpace(v), 80)
		}
		if v, ok := params["message_text"].(string); ok {
			out["has_message_text"] = strings.TrimSpace(v) != ""
		}
		if v, ok := params["message_base64"].(string); ok {
			out["has_message_base64"] = strings.TrimSpace(v) != ""
		}
	case "bash":
		if opts.IncludeToolParams {
			if v, ok := params["cmd"].(string); ok && strings.TrimSpace(v) != "" {
				out["cmd"] = truncateString(strings.TrimSpace(v), 500)
			}
		}
	case "powershell":
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

func toolDisplayArgsSummary(toolName string, params map[string]any, opts LogOptions) map[string]any {
	if len(params) == 0 {
		return nil
	}

	opts = normalizeLogOptions(opts)
	opts.IncludeToolParams = true
	if out := toolArgsSummary(toolName, params, opts, false); len(out) > 0 {
		return out
	}

	maxStr := opts.MaxStringValueChars
	if maxStr <= 0 || maxStr > 240 {
		maxStr = 240
	}
	sanitized, _ := sanitizeValue(params, maxStr, opts.RedactKeys, "").(map[string]any)
	if len(sanitized) == 0 {
		return nil
	}
	return sanitized
}
