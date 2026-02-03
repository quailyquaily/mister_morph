package agent

import (
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

func buildLLMTools(registry *tools.Registry) []llm.Tool {
	if registry == nil {
		return nil
	}
	all := registry.All()
	if len(all) == 0 {
		return nil
	}

	out := make([]llm.Tool, 0, len(all))
	for _, t := range all {
		name := strings.TrimSpace(t.Name())
		if name == "" {
			continue
		}
		out = append(out, llm.Tool{
			Name:           name,
			Description:    strings.TrimSpace(t.Description()),
			ParametersJSON: strings.TrimSpace(t.ParameterSchema()),
		})
	}
	return out
}

func toAgentToolCalls(calls []llm.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		out = append(out, ToolCall{
			ID:     strings.TrimSpace(call.ID),
			Name:   name,
			Params: call.Arguments,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toLLMToolCallsFromAgent(calls []ToolCall) []llm.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]llm.ToolCall, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		out = append(out, llm.ToolCall{
			ID:        strings.TrimSpace(call.ID),
			Name:      name,
			Arguments: call.Params,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
