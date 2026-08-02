package uniai

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lyricat/goutils/structs"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
)

func shouldEnsureGeminiThoughtSignature(provider, _ string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "gemini")
}

func (c *Client) buildChatOptions(req llm.Request, forceJSON bool) []uniaiapi.ChatOption {
	provider := c.provider
	defaultModel := c.model
	cacheTTL := c.cacheTTL
	cacheKeyPrefix := c.cacheKeyPrefix
	toolsEmulationMode := c.toolsEmulationMode
	defaultTemperature := c.temperature
	defaultReasoningEffort := c.reasoningEffort
	defaultReasoningBudget := c.reasoningBudget

	model := firstNonEmpty(req.Model, defaultModel)
	req = adaptRequestForProvider(req, provider, model, cacheTTL)
	msgs := make([]uniaiapi.Message, len(req.Messages))
	for i, m := range req.Messages {
		msg := uniaiapi.Message{Role: m.Role, Content: m.Content, ReasoningContent: m.ReasoningContent}
		if len(m.Parts) > 0 {
			msg.Parts = toUniaiPartsFromLLM(provider, model, m.Parts)
		}
		if strings.TrimSpace(m.ToolCallID) != "" {
			msg.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = toUniaiToolCallsFromLLM(m.ToolCalls)
		}
		msgs[i] = msg
	}

	opts := []uniaiapi.ChatOption{uniaiapi.WithReplaceMessages(msgs...)}
	openAIOptions := structs.JSONMap{}
	azureOptions := structs.JSONMap{}
	if provider != "" {
		opts = append(opts, uniaiapi.WithProvider(provider))
	}
	if strings.TrimSpace(req.Model) != "" {
		opts = append(opts, uniaiapi.WithModel(strings.TrimSpace(req.Model)))
	}
	if strings.TrimSpace(req.InferenceProvider) != "" {
		opts = append(opts, uniaiapi.WithInferenceProvider(strings.TrimSpace(req.InferenceProvider)))
	}

	if len(req.Tools) > 0 {
		tools := make([]uniaiapi.Tool, 0, len(req.Tools))
		for _, t := range req.Tools {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				continue
			}
			tool := uniaiapi.FunctionTool(
				name,
				strings.TrimSpace(t.Description),
				[]byte(t.ParametersJSON),
			)
			if t.CacheControl != nil {
				if ctrl, ok := toUniaiCacheControlForProvider(provider, model, *t.CacheControl); ok {
					tool = uniaiapi.WithToolCacheControl(tool, ctrl)
				}
			}
			tools = append(tools, tool)
		}
		if len(tools) > 0 {
			opts = append(opts, uniaiapi.WithTools(tools))
			opts = append(opts, uniaiapi.WithToolChoice(uniaiapi.ToolChoiceAuto()))
			if toolsEmulationMode != "" && toolsEmulationMode != uniaiapi.ToolsEmulationOff {
				opts = append(opts, uniaiapi.WithToolsEmulationMode(toolsEmulationMode))
			}
		}
	}

	appliedTemperature := false
	if req.Parameters != nil {
		if v, ok := floatFromAny(req.Parameters["temperature"]); ok {
			opts = append(opts, uniaiapi.WithTemperature(v))
			appliedTemperature = true
		}
		if v, ok := floatFromAny(req.Parameters["top_p"]); ok {
			opts = append(opts, uniaiapi.WithTopP(v))
		}
		if v, ok := intFromAny(req.Parameters["max_tokens"]); ok && v > 0 {
			opts = append(opts, uniaiapi.WithMaxTokens(v))
		}
		if v, ok := stringSliceFromAny(req.Parameters["stop"]); ok && len(v) > 0 {
			opts = append(opts, uniaiapi.WithStopWords(v...))
		}
		if v, ok := floatFromAny(req.Parameters["presence_penalty"]); ok {
			opts = append(opts, uniaiapi.WithPresencePenalty(v))
		}
		if v, ok := floatFromAny(req.Parameters["frequency_penalty"]); ok {
			opts = append(opts, uniaiapi.WithFrequencyPenalty(v))
		}
		if v, ok := req.Parameters["user"].(string); ok && strings.TrimSpace(v) != "" {
			opts = append(opts, uniaiapi.WithUser(strings.TrimSpace(v)))
		}
	}
	if !appliedTemperature && defaultTemperature != nil {
		opts = append(opts, uniaiapi.WithTemperature(*defaultTemperature))
	}
	if effort := strings.TrimSpace(defaultReasoningEffort); effort != "" {
		opts = append(opts, uniaiapi.WithReasoningEffort(uniaiapi.ReasoningEffort(effort)))
	}
	if defaultReasoningBudget != nil && !strings.EqualFold(strings.TrimSpace(provider), "openai_resp") {
		opts = append(opts, uniaiapi.WithReasoningBudgetTokens(*defaultReasoningBudget))
	}

	applyPromptCacheOptions(provider, model, cacheTTL, cacheKeyPrefix, req, openAIOptions, azureOptions)
	if forceJSON && len(req.Tools) == 0 {
		openAIOptions["response_format"] = "json_object"
		if strings.EqualFold(strings.TrimSpace(provider), "azure") {
			azureOptions["response_format"] = "json_object"
		}
	}
	if req.Parameters != nil {
		mergeProviderOptions(req.Parameters["openai"], openAIOptions)
		mergeProviderOptions(req.Parameters["azure"], azureOptions)
	}
	if len(openAIOptions) > 0 {
		opts = append(opts, uniaiapi.WithOpenAIOptions(openAIOptions))
	}
	if len(azureOptions) > 0 {
		opts = append(opts, uniaiapi.WithAzureOptions(azureOptions))
	}

	if req.DebugFn != nil {
		opts = append(opts, uniaiapi.WithDebugFn(req.DebugFn))
	}
	if req.ReasoningDetails && supportsReasoningDetails(provider, model, defaultReasoningEffort, defaultReasoningBudget) {
		opts = append(opts, uniaiapi.WithReasoningDetails())
	}
	if req.OnStream != nil && supportsStreaming(provider) {
		opts = append(opts, uniaiapi.WithOnStream(func(ev uniaiapi.StreamEvent) error {
			streamEvent := llm.StreamEvent{
				Delta: ev.Delta,
				Done:  ev.Done,
			}
			if ev.ReasoningDelta != nil {
				streamEvent.ReasoningDelta = &llm.ReasoningDelta{
					Index: ev.ReasoningDelta.Index,
					Type:  llm.ReasoningDeltaType(ev.ReasoningDelta.Type),
					Delta: ev.ReasoningDelta.Delta,
				}
			}
			if ev.ToolCallDelta != nil {
				streamEvent.ToolCallDelta = &llm.StreamToolCallDelta{
					Index:     ev.ToolCallDelta.Index,
					ID:        ev.ToolCallDelta.ID,
					Name:      ev.ToolCallDelta.Name,
					ArgsChunk: ev.ToolCallDelta.ArgsChunk,
				}
			}
			if ev.Usage != nil {
				usage := toLLMUsage(*ev.Usage)
				if providerUsesOpenAICompatibleUsage(provider) {
					if enriched, changed := enrichUsageFromOpenAICompatibleRaw(usage, streamEventRaw(ev)); changed {
						usage = enriched
					}
				}
				streamEvent.Usage = &usage
			}
			return req.OnStream(streamEvent)
		}))
	}

	return opts
}

func supportsReasoningDetails(provider, model, reasoningEffort string, reasoningBudget *int) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "openai":
		return openAIModelMatchesFamily(model, "deepseek") || openAIModelMatchesFamily(model, "kimi")
	case "deepseek":
		return true
	case "openai_resp":
		return openAIModelMatchesFamily(model, "gpt-5") ||
			openAIModelMatchesFamily(model, "o1") ||
			openAIModelMatchesFamily(model, "o3") ||
			openAIModelMatchesFamily(model, "o4")
	case "gemini":
		return openAIModelMatchesFamily(model, "gemini-2-5") || openAIModelMatchesFamily(model, "gemini-3")
	case "anthropic", "bedrock":
		if reasoningBudget != nil || strings.TrimSpace(reasoningEffort) != "" {
			return true
		}
		model = strings.ToLower(strings.ReplaceAll(llm.ShortModelName(model), ".", "-"))
		for _, family := range []string{
			"fable-5",
			"mythos-5",
			"opus-5",
			"sonnet-5",
			"opus-4-8",
			"opus-4-7",
			"opus-4-6",
			"sonnet-4-6",
		} {
			if strings.Contains(model, family) {
				return true
			}
		}
	}
	return false
}

func mergeProviderOptions(raw any, dst structs.JSONMap) {
	if dst == nil {
		return
	}
	switch values := raw.(type) {
	case nil:
		return
	case structs.JSONMap:
		for key, value := range values {
			if strings.TrimSpace(key) != "" {
				dst[key] = value
			}
		}
	case map[string]any:
		for key, value := range values {
			if strings.TrimSpace(key) != "" {
				dst[key] = value
			}
		}
	}
}

func toLLMToolCalls(calls []uniaiapi.ToolCall) []llm.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]llm.ToolCall, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}
		params := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &params); err != nil {
				params = map[string]any{"_raw": call.Function.Arguments}
			}
		}
		out = append(out, llm.ToolCall{
			ID:               call.ID,
			Type:             call.Type,
			Name:             name,
			Arguments:        params,
			RawArguments:     call.Function.Arguments,
			ThoughtSignature: call.ThoughtSignature,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toLLMParts(parts []uniaiapi.Part) []llm.Part {
	if len(parts) == 0 {
		return nil
	}
	out := make([]llm.Part, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type)
		if partType == "" {
			continue
		}
		out = append(out, llm.Part{
			Type:         partType,
			Text:         part.Text,
			URL:          part.URL,
			DataBase64:   part.DataBase64,
			MIMEType:     part.MIMEType,
			CacheControl: toLLMCacheControl(part.CacheControl),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toLLMMessages(messages []uniaiapi.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, llm.Message{
			Role:             msg.Role,
			Content:          msg.Content,
			Parts:            toLLMParts(msg.Parts),
			ToolCallID:       msg.ToolCallID,
			ToolCalls:        toLLMToolCalls(msg.ToolCalls),
			ReasoningContent: msg.ReasoningContent,
		})
	}
	return out
}

func toUniaiPartsFromLLM(provider, model string, parts []llm.Part) []uniaiapi.Part {
	if len(parts) == 0 {
		return nil
	}
	out := make([]uniaiapi.Part, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type)
		if partType == "" {
			continue
		}
		out = append(out, uniaiapi.Part{
			Type:       partType,
			Text:       part.Text,
			URL:        part.URL,
			DataBase64: part.DataBase64,
			MIMEType:   part.MIMEType,
			CacheControl: func() *uniaiapi.CacheControl {
				if part.CacheControl == nil {
					return nil
				}
				ctrl, ok := toUniaiCacheControlForProvider(provider, model, *part.CacheControl)
				if !ok {
					return nil
				}
				return &ctrl
			}(),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toUniaiToolCallsFromLLM(calls []llm.ToolCall) []uniaiapi.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]uniaiapi.ToolCall, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		args := "{}"
		if strings.TrimSpace(call.RawArguments) != "" {
			args = call.RawArguments
		} else if call.Arguments != nil {
			if data, err := json.Marshal(call.Arguments); err == nil {
				args = string(data)
			}
		}
		callType := call.Type
		if strings.TrimSpace(callType) == "" {
			callType = "function"
		}
		out = append(out, uniaiapi.ToolCall{
			ID:               call.ID,
			Type:             callType,
			ThoughtSignature: call.ThoughtSignature,
			Function: uniaiapi.ToolCallFunction{
				Name:      name,
				Arguments: args,
			},
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ensureGeminiToolCallThoughtSignatures(calls []llm.ToolCall) []llm.ToolCall {
	if len(calls) == 0 {
		return calls
	}

	out := append([]llm.ToolCall(nil), calls...)
	lastSig := ""
	for i := range out {
		sig := strings.TrimSpace(out[i].ThoughtSignature)
		if sig == "" {
			_, decoded := splitGeminiToolCallIDAndThoughtSignature(out[i].ID)
			sig = decoded
		}
		if sig == "" {
			sig = lastSig
		}
		if sig == "" {
			sig = synthesizeGeminiThoughtSignature(out[i])
		}
		out[i].ThoughtSignature = sig
		if sig != "" {
			lastSig = sig
		}
	}
	return out
}

func splitGeminiToolCallIDAndThoughtSignature(callID string) (string, string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return "", ""
	}
	idx := strings.LastIndex(callID, "|ts:")
	if idx <= 0 || idx+4 >= len(callID) {
		return callID, ""
	}
	encoded := callID[idx+4:]
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return callID, ""
	}
	baseID := strings.TrimSpace(callID[:idx])
	if baseID == "" {
		return callID, ""
	}
	return baseID, string(decoded)
}

func synthesizeGeminiThoughtSignature(call llm.ToolCall) string {
	seed := strings.TrimSpace(call.ID) + "\n" + strings.TrimSpace(call.Name) + "\n" + strings.TrimSpace(call.RawArguments)
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("mmts_%x", sum[:8])
}
