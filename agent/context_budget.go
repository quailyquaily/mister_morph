package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
)

const (
	contextBudgetDeltaRatio   = 0.7
	maxCompressionAttempts    = 3
	compressedContextMarker   = "[[COMPRESSED_CONTEXT]]"
	compressionSummaryVersion = 1
)

type RequestTokenEstimator interface {
	EstimateRequest(req llm.Request) (int, error)
	EstimateMessages(messages []llm.Message) (int, error)
}

type ContextBudgetConfig struct {
	Provider       string
	Model          string
	ContextWindow  int
	MaxTokenBudget int
	BudgetSource   string
	ContextSource  string
}

type ContextBudgetState struct {
	CurrentTokens           int    `json:"current_tokens,omitempty"`
	ContextWindow           int    `json:"context_window,omitempty"`
	MaxTokenBudget          int    `json:"max_token_budget,omitempty"`
	CompressionCount        int    `json:"compression_count,omitempty"`
	LastFullPreflightTokens int    `json:"last_full_preflight_tokens,omitempty"`
	AddedTokensSinceCheck   int    `json:"added_tokens_since_check,omitempty"`
	PreflightMessageCount   int    `json:"preflight_message_count,omitempty"`
	BudgetSource            string `json:"budget_source,omitempty"`
	ContextSource           string `json:"context_source,omitempty"`
	HasFullPreflight        bool   `json:"has_full_preflight,omitempty"`
}

type compressionSummary struct {
	SummaryVersion int                      `json:"summary_version,omitempty"`
	CurrentTask    string                   `json:"current_task,omitempty"`
	CurrentState   string                   `json:"current_state,omitempty"`
	ImportantFacts []string                 `json:"important_facts,omitempty"`
	OpenItems      []string                 `json:"open_items,omitempty"`
	LookupIndex    []compressionLookupIndex `json:"lookup_index,omitempty"`
}

type compressionLookupIndex struct {
	Label string `json:"label,omitempty"`
	Where string `json:"where,omitempty"`
	Why   string `json:"why,omitempty"`
}

func cloneContextBudgetState(in *ContextBudgetState) *ContextBudgetState {
	if in == nil {
		return nil
	}
	cloned := *in
	return &cloned
}

func (e *Engine) maybePreflightRequest(ctx context.Context, st *engineLoopState, step int, req llm.Request, log *slog.Logger) (llm.Request, error) {
	if e == nil || st == nil || st.agentCtx == nil || st.agentCtx.ContextBudget == nil {
		return req, nil
	}
	if e.tokenEstimator == nil || st.agentCtx.ContextBudget.MaxTokenBudget <= 0 {
		return req, nil
	}
	state := st.agentCtx.ContextBudget
	if !state.HasFullPreflight || state.PreflightMessageCount <= 0 {
		return e.preflightAndMaybeCompress(ctx, st, step, req, log)
	}
	if state.PreflightMessageCount > len(req.Messages) || state.LastFullPreflightTokens <= 0 {
		return e.preflightAndMaybeCompress(ctx, st, step, req, log)
	}

	deltaMessages := req.Messages[state.PreflightMessageCount:]
	deltaTokens, err := e.tokenEstimator.EstimateMessages(deltaMessages)
	if err != nil {
		return req, fmt.Errorf("estimate delta messages: %w", err)
	}
	state.AddedTokensSinceCheck = deltaTokens
	state.CurrentTokens = state.LastFullPreflightTokens + deltaTokens

	threshold := 0
	if state.ContextWindow > 0 {
		threshold = int(math.Ceil(float64(state.ContextWindow) * contextBudgetDeltaRatio))
	}
	if (threshold <= 0 || deltaTokens < threshold) && state.CurrentTokens < state.MaxTokenBudget {
		if log != nil {
			log.Debug(
				"context_preflight_skipped",
				"step", step,
				"current_tokens", state.CurrentTokens,
				"delta_tokens", deltaTokens,
				"delta_threshold", threshold,
				"max_token_budget", state.MaxTokenBudget,
			)
		}
		return req, nil
	}
	return e.preflightAndMaybeCompress(ctx, st, step, req, log)
}

func (e *Engine) preflightAndMaybeCompress(ctx context.Context, st *engineLoopState, step int, req llm.Request, log *slog.Logger) (llm.Request, error) {
	state := st.agentCtx.ContextBudget
	for attempt := 0; attempt < maxCompressionAttempts; attempt++ {
		totalTokens, err := e.tokenEstimator.EstimateRequest(req)
		if err != nil {
			return req, fmt.Errorf("estimate request: %w", err)
		}
		state.CurrentTokens = totalTokens
		state.LastFullPreflightTokens = totalTokens
		state.AddedTokensSinceCheck = 0
		state.PreflightMessageCount = len(req.Messages)
		state.HasFullPreflight = true
		if totalTokens < state.MaxTokenBudget {
			if log != nil {
				log.Debug(
					"context_preflight_ok",
					"step", step,
					"current_tokens", totalTokens,
					"max_token_budget", state.MaxTokenBudget,
					"compression_count", state.CompressionCount,
				)
			}
			return req, nil
		}
		if log != nil {
			log.Warn(
				"context_budget_exceeded",
				"step", step,
				"current_tokens", totalTokens,
				"max_token_budget", state.MaxTokenBudget,
				"compression_count", state.CompressionCount,
				"attempt", attempt+1,
			)
		}

		compressedMessages, err := e.compressMessages(ctx, req.Model, req.Scene, req.Messages, log)
		if err != nil {
			return req, err
		}
		req.Messages = compressedMessages
		st.messages = compressedMessages
		state.CompressionCount++
	}
	return req, fmt.Errorf("request still exceeds max_token_budget after %d compressions", maxCompressionAttempts)
}

func (e *Engine) compressMessages(ctx context.Context, model string, scene string, messages []llm.Message, log *slog.Logger) ([]llm.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}
	systemMessage, hasSystem := firstSystemMessage(messages)
	payload := compressionPayloadMessages(messages)
	if len(payload) == 0 {
		return messages, nil
	}

	summary, err := e.runCompressionRequest(ctx, model, compressionScene(scene), payload)
	if err != nil && isContextLengthLikeError(err) {
		reduced := reducedCompressionPayload(messages)
		if len(reduced) == 0 {
			return nil, err
		}
		if log != nil {
			log.Warn("context_compression_retry_reduced_payload", "messages", len(reduced))
		}
		summary, err = e.runCompressionRequest(ctx, model, compressionScene(scene), reduced)
	}
	if err != nil {
		return nil, err
	}

	out := make([]llm.Message, 0, 2)
	if hasSystem {
		out = append(out, systemMessage)
	}
	out = append(out, buildCompressedContextMessage(summary))
	if log != nil {
		log.Info(
			"context_compressed",
			"messages_before", len(messages),
			"messages_after", len(out),
			"important_facts", len(summary.ImportantFacts),
			"open_items", len(summary.OpenItems),
			"lookup_index", len(summary.LookupIndex),
		)
	}
	return out, nil
}

func (e *Engine) runCompressionRequest(ctx context.Context, model string, scene string, messages []llm.Message) (compressionSummary, error) {
	result, err := e.client.Chat(ctx, llm.Request{
		Model:     model,
		Scene:     scene,
		Messages:  messages,
		ForceJSON: true,
	})
	if err != nil {
		return compressionSummary{}, err
	}
	return parseCompressionSummary(result)
}

func parseCompressionSummary(result llm.Result) (compressionSummary, error) {
	var raw []byte
	switch {
	case result.JSON != nil:
		b, err := json.Marshal(result.JSON)
		if err != nil {
			return compressionSummary{}, fmt.Errorf("marshal compression JSON result: %w", err)
		}
		raw = b
	case strings.TrimSpace(result.Text) != "":
		raw = []byte(strings.TrimSpace(result.Text))
	default:
		return compressionSummary{}, fmt.Errorf("compression result is empty")
	}

	var summary compressionSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return compressionSummary{}, fmt.Errorf("parse compression summary: %w", err)
	}
	if summary.SummaryVersion <= 0 {
		summary.SummaryVersion = compressionSummaryVersion
	}
	summary.CurrentTask = strings.TrimSpace(summary.CurrentTask)
	summary.CurrentState = strings.TrimSpace(summary.CurrentState)
	summary.ImportantFacts = compactNonEmptyStrings(summary.ImportantFacts)
	summary.OpenItems = compactNonEmptyStrings(summary.OpenItems)
	if len(summary.LookupIndex) > 0 {
		cleaned := make([]compressionLookupIndex, 0, len(summary.LookupIndex))
		for _, item := range summary.LookupIndex {
			item.Label = strings.TrimSpace(item.Label)
			item.Where = strings.TrimSpace(item.Where)
			item.Why = strings.TrimSpace(item.Why)
			if item.Label == "" && item.Where == "" && item.Why == "" {
				continue
			}
			cleaned = append(cleaned, item)
		}
		summary.LookupIndex = cleaned
	}
	if summary.CurrentTask == "" && summary.CurrentState == "" && len(summary.ImportantFacts) == 0 && len(summary.OpenItems) == 0 && len(summary.LookupIndex) == 0 {
		return compressionSummary{}, fmt.Errorf("compression summary missing all required fields")
	}
	return summary, nil
}

func buildCompressedContextMessage(summary compressionSummary) llm.Message {
	data, err := json.Marshal(summary)
	if err != nil {
		data = []byte(`{"summary_version":1}`)
	}
	return llm.Message{
		Role: "user",
		Content: compressedContextMarker + "\n" +
			"Earlier context was compressed to fit the model context window. " +
			"The original message-by-message history before this point has been removed. " +
			"Use this JSON as the authoritative summary of earlier context.\n" +
			string(data),
	}
}

func compressionPayloadMessages(messages []llm.Message) []llm.Message {
	payload := make([]llm.Message, 0, len(messages)+1)
	for _, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.Role), "system") {
			continue
		}
		payload = append(payload, msg)
	}
	if len(payload) == 0 {
		return nil
	}
	payload = append(payload, llm.Message{
		Role: "user",
		Content: "Compress all prior conversation context into minimal JSON. Return JSON only with this schema: " +
			`{"summary_version":1,"current_task":"","current_state":"","important_facts":[],"open_items":[],"lookup_index":[{"label":"","where":"","why":""}]}` +
			". Keep only durable information needed to continue the task. " +
			"Use lookup_index to say where to inspect later when exact details were omitted. No markdown, no code fences, no extra keys.",
	})
	return payload
}

func reducedCompressionPayload(messages []llm.Message) []llm.Message {
	nonSystem := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.Role), "system") {
			continue
		}
		nonSystem = append(nonSystem, msg)
	}
	if len(nonSystem) == 0 {
		return nil
	}
	if len(nonSystem) == 1 {
		return compressionPayloadMessages(nonSystem)
	}

	start := len(nonSystem) / 2
	indexes := make([]int, 0, 1+(len(nonSystem)-start))
	indexes = append(indexes, 0)
	for i := start; i < len(nonSystem); i++ {
		indexes = append(indexes, i)
	}
	out := make([]llm.Message, 0, len(indexes))
	seen := make(map[int]bool, len(indexes))
	for _, idx := range indexes {
		if idx < 0 || idx >= len(nonSystem) || seen[idx] {
			continue
		}
		seen[idx] = true
		out = append(out, nonSystem[idx])
	}
	return compressionPayloadMessages(out)
}

func firstSystemMessage(messages []llm.Message) (llm.Message, bool) {
	for _, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.Role), "system") {
			return msg, true
		}
	}
	return llm.Message{}, false
}

func compressionScene(scene string) string {
	scene = strings.TrimSpace(scene)
	if scene == "" {
		return "context.compress"
	}
	return scene + ".context.compress"
}

func compactNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func isContextLengthLikeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	needles := []string{
		"context length",
		"context window",
		"maximum context",
		"too many tokens",
		"too many input tokens",
		"input is too long",
		"prompt is too long",
		"maximum input length",
		"request too large",
		"reduce the length",
		"input token",
		"tokens in the messages",
	}
	for _, needle := range needles {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
