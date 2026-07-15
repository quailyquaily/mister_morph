package agent

import (
	"context"
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

func (e *Engine) forceConclusion(ctx context.Context, st *engineLoopState, log *slog.Logger) (*Final, *Context, error) {
	if st == nil || st.agentCtx == nil {
		return nil, nil, fmt.Errorf("nil engine state")
	}
	agentCtx := st.agentCtx
	if log == nil {
		log = e.log.With("model", st.model)
	}
	steps := len(agentCtx.Steps)
	log.Warn("force_conclusion", "steps", steps, "messages", len(st.messages))
	st.messages = append(st.messages, llm.Message{
		Role:    "user",
		Content: "You have reached the maximum number of steps or token budget. Provide your final output NOW as a JSON final response.",
	})
	st.protectLastMessage()

	result, err := e.callMainWithContextCompaction(ctx, st, agentCtx.MaxSteps, nil)
	if err != nil {
		log.Error("force_conclusion_llm_error", "error", err.Error())
		if e.fallbackFinal != nil {
			return e.fallbackFinal(), agentCtx, nil
		}
		return &Final{
			Output: buildForceConclusionFallbackOutput(steps, summarizeForceConclusionModelError(err)),
			Plan:   agentCtx.Plan,
		}, agentCtx, nil
	}
	agentCtx.AddUsage(result.Usage, result.Duration)

	resp, err := ParseResponse(result)
	if err != nil {
		log.Warn("force_conclusion_parse_error", "error", err.Error())
		if e.fallbackFinal != nil {
			return e.fallbackFinal(), agentCtx, nil
		}
		return &Final{
			Output: buildForceConclusionFallbackOutput(steps, forceConclusionReasonFinalFormat),
			Plan:   agentCtx.Plan,
		}, agentCtx, nil
	}
	if resp.Type != TypeFinal && resp.Type != TypeFinalAnswer {
		log.Warn("force_conclusion_invalid_type", "type", resp.Type)
		if e.fallbackFinal != nil {
			return e.fallbackFinal(), agentCtx, nil
		}
		return &Final{
			Output: buildForceConclusionFallbackOutput(steps, fmt.Sprintf(forceConclusionReasonTypeTemplate, resp.Type)),
			Plan:   agentCtx.Plan,
		}, agentCtx, nil
	}
	agentCtx.RawFinalAnswer = resp.RawFinalAnswer
	log.Info("force_conclusion_final")
	fp := resp.FinalPayload()
	if agentCtx.Plan != nil && fp != nil && fp.Plan == nil {
		fp.Plan = agentCtx.Plan
	}
	return fp, agentCtx, nil
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
	case "contacts_send":
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
