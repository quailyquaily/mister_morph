package agent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mister_morph/llm"
)

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
