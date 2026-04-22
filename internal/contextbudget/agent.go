package contextbudget

import (
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

func BuildAgentContextBudget(values llmutil.RuntimeValues, provider string, model string) (agent.ContextBudgetConfig, agent.RequestTokenEstimator, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = strings.TrimSpace(values.Provider)
	}
	provider = normalizeProvider(provider)
	model = strings.TrimSpace(model)
	if model == "" {
		model = llmutil.ModelForProviderWithValues(provider, values)
	}

	explicitBudget, explicitSource := effectiveExplicitBudget(values, provider, model)
	resolved, err := ResolveBudget(explicitBudget, explicitSource, provider, model)
	if err != nil {
		return agent.ContextBudgetConfig{}, nil, err
	}
	cfg := agent.ContextBudgetConfig{
		Provider:       resolved.Provider,
		Model:          model,
		ContextWindow:  resolved.ContextWindow,
		MaxTokenBudget: resolved.MaxTokenBudget,
		BudgetSource:   resolved.BudgetSource,
		ContextSource:  resolved.ContextSource,
	}
	if strings.TrimSpace(model) == "" {
		return cfg, nil, nil
	}

	estimator, err := NewEstimator(provider, model, values.ToolsEmulationMode)
	if err != nil {
		return agent.ContextBudgetConfig{}, nil, err
	}
	return cfg, estimator, nil
}

func effectiveExplicitBudget(values llmutil.RuntimeValues, provider string, model string) (*int, string) {
	source := strings.TrimSpace(values.MaxTokenBudgetSource)
	if source == "" || !strings.HasPrefix(source, "llm.profiles.") {
		return cloneOptionalInt(values.MaxTokenBudget), source
	}

	configuredProvider := normalizeProvider(values.Provider)
	configuredModel := llmutil.ModelForProviderWithValues(configuredProvider, values)
	if normalizeProvider(provider) == configuredProvider && normalizeModelKey(model) == normalizeModelKey(configuredModel) {
		return cloneOptionalInt(values.MaxTokenBudget), source
	}

	return cloneOptionalInt(values.GlobalMaxTokenBudget), strings.TrimSpace(values.GlobalMaxTokenBudgetSource)
}

func cloneOptionalInt(v *int) *int {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
