package contextbudget

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

func TestBuildAgentContextBudget_IgnoresProfileBudgetWhenModelOverrides(t *testing.T) {
	cfg, _, err := BuildAgentContextBudget(llmutil.RuntimeValues{
		Provider:                   "openai",
		Model:                      "gpt-4.1-mini",
		MaxTokenBudget:             intPtr(80000),
		MaxTokenBudgetSource:       "llm.profiles.cheap.max_token_budget",
		GlobalMaxTokenBudget:       intPtr(160000),
		GlobalMaxTokenBudgetSource: "llm.max_token_budget",
	}, "openai", "gpt-5.2")
	if err != nil {
		t.Fatalf("BuildAgentContextBudget() error = %v", err)
	}
	if cfg.MaxTokenBudget != 160000 {
		t.Fatalf("max_token_budget = %d, want 160000", cfg.MaxTokenBudget)
	}
	if cfg.BudgetSource != "llm.max_token_budget" {
		t.Fatalf("budget_source = %q, want llm.max_token_budget", cfg.BudgetSource)
	}
}

func TestBuildAgentContextBudget_FallsBackToCatalogAfterProfileBudgetInvalidates(t *testing.T) {
	cfg, _, err := BuildAgentContextBudget(llmutil.RuntimeValues{
		Provider:             "openai",
		Model:                "gpt-4.1-mini",
		MaxTokenBudget:       intPtr(80000),
		MaxTokenBudgetSource: "llm.profiles.cheap.max_token_budget",
	}, "openai", "gpt-5.2")
	if err != nil {
		t.Fatalf("BuildAgentContextBudget() error = %v", err)
	}
	if cfg.ContextWindow != 400000 {
		t.Fatalf("context_window = %d, want 400000", cfg.ContextWindow)
	}
	if cfg.MaxTokenBudget != 320000 {
		t.Fatalf("max_token_budget = %d, want 320000", cfg.MaxTokenBudget)
	}
	if cfg.BudgetSource != "builtin_context_window:gpt-5.2" {
		t.Fatalf("budget_source = %q, want builtin_context_window:gpt-5.2", cfg.BudgetSource)
	}
}

func TestBuildAgentContextBudget_UnknownModelUsesFallback(t *testing.T) {
	cfg, _, err := BuildAgentContextBudget(llmutil.RuntimeValues{
		Provider: "openai",
		Model:    "unknown-model",
	}, "openai", "unknown-model")
	if err != nil {
		t.Fatalf("BuildAgentContextBudget() error = %v", err)
	}
	if cfg.ContextWindow != defaultContextWindowTokens {
		t.Fatalf("context_window = %d, want %d", cfg.ContextWindow, defaultContextWindowTokens)
	}
	wantBudget := int(float64(defaultContextWindowTokens) * budgetRatio)
	if cfg.MaxTokenBudget != wantBudget {
		t.Fatalf("max_token_budget = %d, want %d", cfg.MaxTokenBudget, wantBudget)
	}
	if cfg.BudgetSource != defaultFallbackBudgetSource {
		t.Fatalf("budget_source = %q, want %q", cfg.BudgetSource, defaultFallbackBudgetSource)
	}
}

func TestBuildAgentContextBudget_AzureDeploymentMissUsesFallback(t *testing.T) {
	cfg, _, err := BuildAgentContextBudget(llmutil.RuntimeValues{
		Provider:        "azure",
		Model:           "gpt-4.1-mini",
		AzureDeployment: "team-prod-deploy",
	}, "azure", "team-prod-deploy")
	if err != nil {
		t.Fatalf("BuildAgentContextBudget() error = %v", err)
	}
	if cfg.ContextWindow != defaultContextWindowTokens {
		t.Fatalf("context_window = %d, want %d", cfg.ContextWindow, defaultContextWindowTokens)
	}
	if cfg.BudgetSource != defaultFallbackBudgetSource {
		t.Fatalf("budget_source = %q, want %q", cfg.BudgetSource, defaultFallbackBudgetSource)
	}
}

func TestBuildAgentContextBudget_AzureDeploymentMissHonorsManualBudget(t *testing.T) {
	cfg, _, err := BuildAgentContextBudget(llmutil.RuntimeValues{
		Provider:                   "azure",
		Model:                      "gpt-4.1-mini",
		AzureDeployment:            "team-prod-deploy",
		MaxTokenBudget:             intPtr(90000),
		MaxTokenBudgetSource:       "llm.max_token_budget",
		GlobalMaxTokenBudget:       intPtr(90000),
		GlobalMaxTokenBudgetSource: "llm.max_token_budget",
	}, "azure", "team-prod-deploy")
	if err != nil {
		t.Fatalf("BuildAgentContextBudget() error = %v", err)
	}
	if cfg.MaxTokenBudget != 90000 {
		t.Fatalf("max_token_budget = %d, want 90000", cfg.MaxTokenBudget)
	}
	if cfg.BudgetSource != "llm.max_token_budget" {
		t.Fatalf("budget_source = %q, want llm.max_token_budget", cfg.BudgetSource)
	}
}

func intPtr(v int) *int {
	return &v
}
