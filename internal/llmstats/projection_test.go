package llmstats

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	uniaiapi "github.com/quailyquaily/uniai"
	"github.com/spf13/viper"
)

func TestProjectionStoreCapturesPricingPathsAtConstruction(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	projectionPath := filepath.Join(root, "projection.json")
	configA := filepath.Join(root, "a", "config.yaml")
	pricingA := filepath.Join(root, "a", "pricing.yaml")
	configB := filepath.Join(root, "b", "config.yaml")
	pricingB := filepath.Join(root, "b", "pricing.yaml")
	for _, dir := range []string{filepath.Dir(configA), filepath.Dir(configB)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}
	pricingYAML := func(inputPrice int) []byte {
		return []byte("version: uniai.pricing.v1\nchat:\n  - inference_provider: openai\n    model: test-model\n    input_usd_per_million: " + fmt.Sprint(inputPrice) + "\n    output_usd_per_million: 0\n")
	}
	if err := os.WriteFile(pricingA, pricingYAML(1), 0o600); err != nil {
		t.Fatalf("WriteFile(pricing A) error = %v", err)
	}
	if err := os.WriteFile(pricingB, pricingYAML(9), 0o600); err != nil {
		t.Fatalf("WriteFile(pricing B) error = %v", err)
	}
	journal := NewJournal(journalDir, JournalOptions{})
	if _, err := journal.Append(RequestRecord{
		TS:          time.Now().UTC().Format(time.RFC3339),
		Provider:    "openai",
		APIBase:     "https://example.test",
		Model:       "test-model",
		Operation:   operationChat,
		InputTokens: 1_000_000,
		TotalTokens: 1_000_000,
	}); err != nil {
		t.Fatalf("Journal.Append() error = %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("Journal.Close() error = %v", err)
	}

	viper.Set("config", configA)
	viper.Set("llm.pricing_file", "pricing.yaml")
	store := NewProjectionStore(journalDir, projectionPath)
	viper.Set("config", configB)
	viper.Set("llm.pricing_file", "pricing.yaml")

	projection, err := store.Refresh()
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !costAlmostEqual(projection.Summary.TotalCost, 1) {
		t.Fatalf("total cost = %v, want captured pricing cost 1", projection.Summary.TotalCost)
	}
}

func TestProjectionRefreshAggregatesAndReplaysTail(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	projectionPath := filepath.Join(root, "projection.json")
	journal := NewJournal(journalDir, JournalOptions{MaxFileBytes: 1024 * 1024})
	journal.now = func() time.Time {
		return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	}
	defer func() { _ = journal.Close() }()

	appendRecord := func(host, model string, input, output int64) {
		t.Helper()
		_, err := journal.Append(RequestRecord{
			TS:                       time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
			Provider:                 "openai",
			APIBase:                  "https://" + host,
			Model:                    model,
			InputTokens:              input,
			OutputTokens:             output,
			TotalTokens:              input + output,
			CachedInputTokens:        input / 2,
			CacheCreationInputTokens: output / 2,
			CacheDetails: map[string]int64{
				"ephemeral_5m_input_tokens": output / 2,
			},
			CostCurrency:           "USD",
			CostEstimated:          true,
			InputCost:              float64(input) / 1000,
			CachedInputCost:        float64(input/2) / 1000,
			CacheCreationInputCost: float64(output/2) / 1000,
			OutputCost:             float64(output) / 1000,
			TotalCost:              float64(input+output) / 1000,
		})
		if err != nil {
			t.Fatalf("Append(%s,%s) error = %v", host, model, err)
		}
	}

	appendRecord("api.openai.com", "gpt-5.2", 10, 5)
	appendRecord("api.openai.com", "gpt-5-mini", 20, 10)

	store := NewProjectionStore(journalDir, projectionPath)
	store.now = func() time.Time {
		return time.Date(2026, 3, 7, 12, 30, 0, 0, time.UTC)
	}
	proj, err := store.Refresh()
	if err != nil {
		t.Fatalf("Refresh(1) error = %v", err)
	}
	if proj.Summary.Requests != 2 || proj.Summary.TotalTokens != 45 {
		t.Fatalf("projection1 summary = %+v, want requests=2 total_tokens=45", proj.Summary)
	}
	if proj.Summary.CachedInputTokens != 15 || proj.Summary.CacheCreationInputTokens != 7 {
		t.Fatalf("projection1 cache totals = %+v", proj.Summary)
	}
	if proj.Summary.CostCurrency != "USD" || !costAlmostEqual(proj.Summary.TotalCost, 0.045) {
		t.Fatalf("projection1 cost totals = %+v", proj.Summary)
	}
	if len(proj.APIHosts) != 1 || proj.APIHosts[0].APIHost != "api.openai.com" {
		t.Fatalf("projection1 hosts = %+v", proj.APIHosts)
	}
	if len(proj.Models) != 2 {
		t.Fatalf("len(projection1 models) = %d, want 2", len(proj.Models))
	}

	appendRecord("api.openai.com", "gpt-5.2", 3, 2)
	proj, err = store.Refresh()
	if err != nil {
		t.Fatalf("Refresh(2) error = %v", err)
	}
	if proj.Summary.Requests != 3 || proj.Summary.TotalTokens != 50 {
		t.Fatalf("projection2 summary = %+v, want requests=3 total_tokens=50", proj.Summary)
	}
	if proj.Summary.CachedInputTokens != 16 || proj.Summary.CacheCreationInputTokens != 8 {
		t.Fatalf("projection2 cache totals = %+v", proj.Summary)
	}
	if !costAlmostEqual(proj.Summary.TotalCost, 0.05) {
		t.Fatalf("projection2 cost totals = %+v", proj.Summary)
	}
	if proj.ProjectedOffset.File == "" || proj.ProjectedOffset.Line != 3 {
		t.Fatalf("projection2 offset = %+v, want line 3", proj.ProjectedOffset)
	}
	segmentInfo, err := os.Stat(filepath.Join(journalDir, proj.ProjectedOffset.File))
	if err != nil {
		t.Fatalf("Stat(projected segment) error = %v", err)
	}
	if proj.ProjectedOffset.Byte != segmentInfo.Size() {
		t.Fatalf("projection2 byte offset = %d, want %d", proj.ProjectedOffset.Byte, segmentInfo.Size())
	}
}

func TestProjectionRefreshWarmReadDoesNotReadOrRewrite(t *testing.T) {
	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	projectionPath := filepath.Join(root, "projection.json")
	journal := NewJournal(journalDir, JournalOptions{})
	if _, err := journal.Append(RequestRecord{
		TS:          time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Provider:    "openai",
		APIBase:     "https://api.openai.com",
		Model:       "gpt-5.2",
		InputTokens: 4,
		TotalTokens: 4,
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store := NewProjectionStore(journalDir, projectionPath)
	first, err := store.Refresh()
	if err != nil {
		t.Fatalf("Refresh(1) error = %v", err)
	}
	fixedModTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(projectionPath, fixedModTime, fixedModTime); err != nil {
		t.Fatalf("Chtimes(projection) error = %v", err)
	}
	segmentPath := filepath.Join(journalDir, first.ProjectedOffset.File)
	if err := os.Chmod(segmentPath, 0); err != nil {
		t.Fatalf("Chmod(segment) error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(segmentPath, 0o600) })

	second, err := store.Refresh()
	if err != nil {
		t.Fatalf("Refresh(2) warm read error = %v", err)
	}
	if second.ProjectedOffset != first.ProjectedOffset || second.Summary.Requests != first.Summary.Requests {
		t.Fatalf("warm projection changed: first=%+v second=%+v", first, second)
	}
	info, err := os.Stat(projectionPath)
	if err != nil {
		t.Fatalf("Stat(projection) error = %v", err)
	}
	if !info.ModTime().Equal(fixedModTime) {
		t.Fatalf("projection mod time = %s, want unchanged %s", info.ModTime(), fixedModTime)
	}
}

func TestProjectionRefreshIgnoresIncompleteTail(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	segmentPath := filepath.Join(journalDir, "since-2026-03-07-0001.jsonl")
	content := "{\"ts\":\"2026-03-07T12:00:00Z\",\"provider\":\"openai\",\"api_host\":\"api.openai.com\",\"model\":\"gpt-5.2\",\"input_tokens\":10,\"output_tokens\":5,\"total_tokens\":15}\n" +
		"{\"ts\":\"2026-03-07T12:01:00Z\",\"provider\":\"openai\",\"api_host\":\"api.openai.com\",\"model\":\"gpt-5-mini\""
	if err := os.WriteFile(segmentPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := NewProjectionStore(journalDir, filepath.Join(root, "projection.json"))
	proj, err := store.Refresh()
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if proj.Summary.Requests != 1 || proj.Summary.TotalTokens != 15 {
		t.Fatalf("projection summary = %+v, want one committed record", proj.Summary)
	}
	if proj.ProjectedOffset.File != "since-2026-03-07-0001.jsonl" || proj.ProjectedOffset.Line != 1 {
		t.Fatalf("projection offset = %+v, want first line only", proj.ProjectedOffset)
	}
	committedBytes := int64(len(content) - len("{\"ts\":\"2026-03-07T12:01:00Z\",\"provider\":\"openai\",\"api_host\":\"api.openai.com\",\"model\":\"gpt-5-mini\""))
	if proj.ProjectedOffset.Byte != committedBytes {
		t.Fatalf("projection byte offset = %d, want committed bytes %d", proj.ProjectedOffset.Byte, committedBytes)
	}
}

func TestProjectionRefreshBackfillsLegacyCostFromPricing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	projectionPath := filepath.Join(root, "projection.json")
	journal := NewJournal(journalDir, JournalOptions{MaxFileBytes: 1024 * 1024})
	defer func() { _ = journal.Close() }()

	if _, err := journal.Append(RequestRecord{
		TS:           time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Provider:     "openai",
		APIBase:      "https://api.openai.com",
		Model:        "gpt-5.4",
		InputTokens:  1000,
		OutputTokens: 2000,
		TotalTokens:  3000,
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	pricing := mustParsePricingCatalog(t, `
version: uniai.pricing.v1
chat:
  - inference_provider: openai
    model: gpt-5.4
    input_usd_per_million: 1
    output_usd_per_million: 2
`)
	store := NewProjectionStore(journalDir, projectionPath)
	store.loadPricing = func() (*uniaiapi.PricingCatalog, string, error) {
		return pricing, "digest-a", nil
	}

	proj, err := store.Refresh()
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if proj.Summary.CostCurrency != "USD" || !costAlmostEqual(proj.Summary.TotalCost, 0.005) {
		t.Fatalf("projection summary cost = %+v", proj.Summary)
	}
	if proj.Summary.InputCost != 0.001 || proj.Summary.OutputCost != 0.004 {
		t.Fatalf("projection summary breakdown = %+v", proj.Summary)
	}
}

func TestProjectionRefreshDoesNotBackfillImageCostFromChatPricing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	projectionPath := filepath.Join(root, "projection.json")
	journal := NewJournal(journalDir, JournalOptions{MaxFileBytes: 1024 * 1024})
	defer func() { _ = journal.Close() }()

	if _, err := journal.Append(RequestRecord{
		TS:           time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Provider:     "openai",
		APIBase:      "https://api.openai.com",
		Model:        "gpt-image-1",
		Operation:    operationImageGenerate,
		InputTokens:  1000,
		OutputTokens: 2000,
		TotalTokens:  3000,
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	pricing := mustParsePricingCatalog(t, `
version: uniai.pricing.v1
chat:
  - inference_provider: openai
    model: gpt-image-1
    input_usd_per_million: 1
    output_usd_per_million: 2
`)
	store := NewProjectionStore(journalDir, projectionPath)
	store.loadPricing = func() (*uniaiapi.PricingCatalog, string, error) {
		return pricing, "digest-a", nil
	}

	proj, err := store.Refresh()
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if proj.Summary.Requests != 1 || proj.Summary.TotalTokens != 3000 {
		t.Fatalf("projection summary usage = %+v", proj.Summary)
	}
	if proj.Summary.CostCurrency != "" || proj.Summary.TotalCost != 0 {
		t.Fatalf("projection summary cost = %+v", proj.Summary)
	}
}

func TestProjectionRefreshRebuildsWhenPricingDigestChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	journalDir := filepath.Join(root, "journal")
	projectionPath := filepath.Join(root, "projection.json")
	journal := NewJournal(journalDir, JournalOptions{MaxFileBytes: 1024 * 1024})
	defer func() { _ = journal.Close() }()

	if _, err := journal.Append(RequestRecord{
		TS:           time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Provider:     "openai",
		APIBase:      "https://api.openai.com",
		Model:        "gpt-5.4",
		InputTokens:  1000,
		OutputTokens: 2000,
		TotalTokens:  3000,
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	store := NewProjectionStore(journalDir, projectionPath)
	store.loadPricing = func() (*uniaiapi.PricingCatalog, string, error) {
		return mustParsePricingCatalog(t, `
version: uniai.pricing.v1
chat:
  - inference_provider: openai
    model: gpt-5.4
    input_usd_per_million: 1
    output_usd_per_million: 2
`), "digest-a", nil
	}
	proj, err := store.Refresh()
	if err != nil {
		t.Fatalf("Refresh(1) error = %v", err)
	}
	if !costAlmostEqual(proj.Summary.TotalCost, 0.005) {
		t.Fatalf("projection1 summary cost = %+v", proj.Summary)
	}

	store.loadPricing = func() (*uniaiapi.PricingCatalog, string, error) {
		return mustParsePricingCatalog(t, `
version: uniai.pricing.v1
chat:
  - inference_provider: openai
    model: gpt-5.4
    input_usd_per_million: 2
    output_usd_per_million: 3
`), "digest-b", nil
	}
	proj, err = store.Refresh()
	if err != nil {
		t.Fatalf("Refresh(2) error = %v", err)
	}
	if !costAlmostEqual(proj.Summary.TotalCost, 0.008) {
		t.Fatalf("projection2 summary cost = %+v", proj.Summary)
	}
	if proj.PricingDigest != "digest-b" {
		t.Fatalf("projection2 pricing digest = %q, want digest-b", proj.PricingDigest)
	}
}

func mustParsePricingCatalog(t *testing.T, yamlText string) *uniaiapi.PricingCatalog {
	t.Helper()
	pricing, err := uniaiapi.ParsePricingYAML([]byte(yamlText))
	if err != nil {
		t.Fatalf("ParsePricingYAML() error = %v", err)
	}
	return pricing
}
