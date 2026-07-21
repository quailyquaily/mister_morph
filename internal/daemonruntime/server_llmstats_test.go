package daemonruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/spf13/viper"
)

func TestLLMUsageStatsRoute(t *testing.T) {
	stateDir := t.TempDir()
	paths := testRuntimePaths(stateDir)

	journal := llmstats.NewJournal(paths.LLMUsageJournalDir, llmstats.JournalOptions{})
	defer func() { _ = journal.Close() }()
	if _, err := journal.Append(llmstats.RequestRecord{
		TS:                       time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Provider:                 "openai",
		APIBase:                  "https://api.openai.com",
		Model:                    "gpt-5.2",
		InputTokens:              8,
		OutputTokens:             4,
		TotalTokens:              12,
		CachedInputTokens:        2,
		CacheCreationInputTokens: 1,
		CostCurrency:             "USD",
		CostEstimated:            true,
		CachedInputCost:          0.002,
		CacheCreationInputCost:   0.001,
		TotalCost:                0.015,
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{Mode: "serve", AuthToken: "token", RuntimePaths: paths})

	req := httptest.NewRequest(http.MethodGet, "/stats/llm/usage", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Summary struct {
			Requests                 int64   `json:"requests"`
			TotalTokens              int64   `json:"total_tokens"`
			CachedInputTokens        int64   `json:"cached_input_tokens"`
			CacheCreationInputTokens int64   `json:"cache_creation_input_tokens"`
			CostCurrency             string  `json:"cost_currency"`
			TotalCost                float64 `json:"total_cost"`
		} `json:"summary"`
		APIHosts []struct {
			APIHost string `json:"api_host"`
		} `json:"api_hosts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Summary.Requests != 1 || payload.Summary.TotalTokens != 12 {
		t.Fatalf("summary = %+v", payload.Summary)
	}
	if payload.Summary.CachedInputTokens != 2 || payload.Summary.CacheCreationInputTokens != 1 {
		t.Fatalf("summary cache = %+v", payload.Summary)
	}
	if payload.Summary.CostCurrency != "USD" || payload.Summary.TotalCost < 0.014999 || payload.Summary.TotalCost > 0.015001 {
		t.Fatalf("summary cost = %+v", payload.Summary)
	}
	if len(payload.APIHosts) != 1 || payload.APIHosts[0].APIHost != "api.openai.com" {
		t.Fatalf("api_hosts = %+v", payload.APIHosts)
	}
}

func TestLLMUsageStatsRouteUsesCapturedPricingConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	stateDir := t.TempDir()
	paths := testRuntimePaths(stateDir)
	configDirA := filepath.Join(t.TempDir(), "a")
	configDirB := filepath.Join(t.TempDir(), "b")
	for _, dir := range []string{configDirA, configDirB} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}
	writePricing := func(path, inputPrice string) {
		t.Helper()
		content := "version: uniai.pricing.v1\nchat:\n  - inference_provider: openai\n    model: test-model\n    input_usd_per_million: " + inputPrice + "\n    output_usd_per_million: 0\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}
	writePricing(filepath.Join(configDirA, "pricing.yaml"), "1")
	writePricing(filepath.Join(configDirB, "pricing.yaml"), "9")

	journal := llmstats.NewJournal(paths.LLMUsageJournalDir, llmstats.JournalOptions{})
	if _, err := journal.Append(llmstats.RequestRecord{
		TS:          time.Now().UTC().Format(time.RFC3339),
		Provider:    "openai",
		APIBase:     "https://example.test",
		Model:       "test-model",
		Operation:   "chat",
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("Journal.Close() error = %v", err)
	}

	settings := viper.New()
	settings.Set("config", filepath.Join(configDirA, "config.yaml"))
	settings.Set("llm.pricing_file", "pricing.yaml")
	viper.Set("config", filepath.Join(configDirB, "config.yaml"))
	viper.Set("llm.pricing_file", "pricing.yaml")

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:                "serve",
		AuthToken:           "token",
		RuntimePaths:        paths,
		AgentSettingsReader: settings,
	})
	req := httptest.NewRequest(http.MethodGet, "/stats/llm/usage", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var payload struct {
		Summary struct {
			TotalCost float64 `json:"total_cost"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Summary.TotalCost < 0.999999 || payload.Summary.TotalCost > 1.000001 {
		t.Fatalf("total cost = %v, want captured pricing cost 1", payload.Summary.TotalCost)
	}
}
