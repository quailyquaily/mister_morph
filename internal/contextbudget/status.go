package contextbudget

import (
	"encoding/json"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/llm"
)

type Status struct {
	CurrentTokens    int `json:"current_tokens"`
	ContextWindow    int `json:"context_window"`
	CompressionCount int `json:"compression_count"`
}

func EstimateStatus(cfg agent.ContextBudgetConfig, estimator agent.RequestTokenEstimator, req llm.Request, compressionCount int) (Status, error) {
	status := Status{
		ContextWindow:    cfg.ContextWindow,
		CompressionCount: compressionCount,
	}
	if estimator == nil {
		return status, nil
	}
	tokens, err := estimator.EstimateRequest(req)
	if err != nil {
		return Status{}, err
	}
	status.CurrentTokens = tokens
	return status, nil
}

func FormatStatusJSON(status Status) string {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}
