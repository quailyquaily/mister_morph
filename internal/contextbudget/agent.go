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
	model = strings.TrimSpace(model)
	if model == "" {
		model = llmutil.ModelForProviderWithValues(provider, values)
	}

	resolved, err := ResolveBudget(values.MaxTokenBudget, values.MaxTokenBudgetSource, provider, model)
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
