package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
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
	case "memory_recently":
		if v, ok := summaryInt(params, "days"); ok {
			out["days"] = v
		}
		if v, ok := summaryInt(params, "limit"); ok {
			out["limit"] = v
		}
		if v, ok := params["include_body"].(bool); ok {
			out["include_body"] = v
		}
	case "read_file":
		if v, ok := params["path"].(string); ok && strings.TrimSpace(v) != "" {
			out["path"] = truncateString(strings.TrimSpace(v), opts.MaxStringValueChars)
		}
	case "contacts_list":
		if v, ok := params["status"].(string); ok && strings.TrimSpace(v) != "" {
			out["status"] = truncateString(strings.TrimSpace(v), 40)
		}
		if v, ok := summaryInt(params, "limit"); ok {
			out["limit"] = v
		}
	case "contacts_candidate_rank":
		if v, ok := summaryInt(params, "limit"); ok {
			out["limit"] = v
		}
		if v, ok := params["freshness_window"].(string); ok && strings.TrimSpace(v) != "" {
			out["freshness_window"] = truncateString(strings.TrimSpace(v), 40)
		}
		if v, ok := summaryFloat(params, "freshness_window_hours"); ok {
			out["freshness_window_hours"] = v
		}
		if v, ok := params["human_enabled"].(bool); ok {
			out["human_enabled"] = v
		}
		if v, ok := params["human_public_send_enabled"].(bool); ok {
			out["human_public_send_enabled"] = v
		}
		if v, ok := params["push_topic"].(string); ok && strings.TrimSpace(v) != "" {
			out["push_topic"] = truncateString(strings.TrimSpace(v), 80)
		}
	case "contacts_send":
		if v, ok := params["contact_id"].(string); ok && strings.TrimSpace(v) != "" {
			out["contact_id"] = truncateString(strings.TrimSpace(v), opts.MaxStringValueChars)
		}
		if v, ok := params["topic"].(string); ok && strings.TrimSpace(v) != "" {
			out["topic"] = truncateString(strings.TrimSpace(v), 80)
		}
		if v, ok := params["content_type"].(string); ok && strings.TrimSpace(v) != "" {
			out["content_type"] = truncateString(strings.TrimSpace(v), 80)
		}
		if v, ok := params["message_text"].(string); ok {
			out["has_message_text"] = strings.TrimSpace(v) != ""
		}
		if v, ok := params["payload_base64"].(string); ok {
			out["has_payload_base64"] = strings.TrimSpace(v) != ""
		}
		if v, ok := summaryInt(params, "source_chat_id"); ok {
			out["source_chat_id"] = v
		}
		if v, ok := params["source_chat_type"].(string); ok && strings.TrimSpace(v) != "" {
			out["source_chat_type"] = truncateString(strings.TrimSpace(v), 32)
		}
	case "contacts_feedback_update":
		if v, ok := params["contact_id"].(string); ok && strings.TrimSpace(v) != "" {
			out["contact_id"] = truncateString(strings.TrimSpace(v), opts.MaxStringValueChars)
		}
		if v, ok := params["signal"].(string); ok && strings.TrimSpace(v) != "" {
			out["signal"] = truncateString(strings.TrimSpace(v), 32)
		}
		if v, ok := params["topic"].(string); ok && strings.TrimSpace(v) != "" {
			out["topic"] = truncateString(strings.TrimSpace(v), 80)
		}
		if v, ok := params["session_id"].(string); ok && strings.TrimSpace(v) != "" {
			out["session_id"] = truncateString(strings.TrimSpace(v), 120)
		}
		if v, ok := params["end_session"].(bool); ok {
			out["end_session"] = v
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

func summaryInt(params map[string]any, key string) (int64, bool) {
	raw, ok := params[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch x := raw.(type) {
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		return int64(x), true
	case float32:
		return int64(x), true
	case float64:
		return int64(x), true
	case string:
		text := strings.TrimSpace(x)
		if text == "" {
			return 0, false
		}
		n, err := strconv.ParseInt(text, 10, 64)
		return n, err == nil
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", x))
		if text == "" {
			return 0, false
		}
		n, err := strconv.ParseInt(text, 10, 64)
		return n, err == nil
	}
}

func summaryFloat(params map[string]any, key string) (float64, bool) {
	raw, ok := params[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch x := raw.(type) {
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		text := strings.TrimSpace(x)
		if text == "" {
			return 0, false
		}
		n, err := strconv.ParseFloat(text, 64)
		return n, err == nil
	default:
		return 0, false
	}
}
