package llmstats

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	unknownProvider = "unknown"
	unknownModel    = "unknown"
	unknownScene    = "unknown"

	operationChat          = "chat"
	operationImageGenerate = "image_generate"
	operationImageEdit     = "image_edit"
)

type Offset struct {
	File string `json:"file,omitempty"`
	Line int64  `json:"line,omitempty"`
	Byte int64  `json:"byte,omitempty"`
}

type RequestRecord struct {
	TS                       string           `json:"ts"`
	RunID                    string           `json:"run_id,omitempty"`
	OriginEventID            string           `json:"origin_event_id,omitempty"`
	Provider                 string           `json:"provider"`
	APIBase                  string           `json:"api_base,omitempty"`
	APIHost                  string           `json:"api_host"`
	Model                    string           `json:"model"`
	Operation                string           `json:"operation,omitempty"`
	Scene                    string           `json:"scene,omitempty"`
	InputTokens              int64            `json:"input_tokens"`
	OutputTokens             int64            `json:"output_tokens"`
	TotalTokens              int64            `json:"total_tokens"`
	CachedInputTokens        int64            `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int64            `json:"cache_creation_input_tokens,omitempty"`
	CacheDetails             map[string]int64 `json:"cache_details,omitempty"`
	CostCurrency             string           `json:"cost_currency,omitempty"`
	CostEstimated            bool             `json:"cost_estimated,omitempty"`
	InputCost                float64          `json:"input_cost,omitempty"`
	CachedInputCost          float64          `json:"cached_input_cost,omitempty"`
	CacheCreationInputCost   float64          `json:"cache_creation_input_cost,omitempty"`
	OutputCost               float64          `json:"output_cost,omitempty"`
	TotalCost                float64          `json:"total_cost,omitempty"`
	DurationMs               int64            `json:"duration_ms,omitempty"`
}

type Totals struct {
	Requests                 int64            `json:"requests"`
	InputTokens              int64            `json:"input_tokens"`
	OutputTokens             int64            `json:"output_tokens"`
	TotalTokens              int64            `json:"total_tokens"`
	CachedInputTokens        int64            `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int64            `json:"cache_creation_input_tokens,omitempty"`
	CacheDetails             map[string]int64 `json:"cache_details,omitempty"`
	CostCurrency             string           `json:"cost_currency,omitempty"`
	CostEstimated            bool             `json:"cost_estimated,omitempty"`
	InputCost                float64          `json:"input_cost,omitempty"`
	CachedInputCost          float64          `json:"cached_input_cost,omitempty"`
	CacheCreationInputCost   float64          `json:"cache_creation_input_cost,omitempty"`
	OutputCost               float64          `json:"output_cost,omitempty"`
	TotalCost                float64          `json:"total_cost,omitempty"`
}

type ModelSummary struct {
	Model string `json:"model"`
	Totals
}

type APIHostSummary struct {
	APIHost string `json:"api_host"`
	Totals
	Models []ModelSummary `json:"models,omitempty"`
}

type Projection struct {
	SchemaVersion    int              `json:"schema_version,omitempty"`
	PricingDigest    string           `json:"pricing_digest,omitempty"`
	UpdatedAt        string           `json:"updated_at,omitempty"`
	ProjectedOffset  Offset           `json:"projected_offset,omitempty"`
	ProjectedRecords int64            `json:"projected_records,omitempty"`
	SkippedRecords   int64            `json:"skipped_records,omitempty"`
	Summary          Totals           `json:"summary"`
	APIHosts         []APIHostSummary `json:"api_hosts,omitempty"`
	Models           []ModelSummary   `json:"models,omitempty"`
}

func (t *Totals) AddRecord(rec RequestRecord) {
	if t == nil {
		return
	}
	t.Requests++
	t.InputTokens += nonNegative(rec.InputTokens)
	t.OutputTokens += nonNegative(rec.OutputTokens)
	t.TotalTokens += nonNegative(rec.TotalTokens)
	t.CachedInputTokens += nonNegative(rec.CachedInputTokens)
	t.CacheCreationInputTokens += nonNegative(rec.CacheCreationInputTokens)
	t.CostCurrency = mergeCostCurrency(t.CostCurrency, rec.CostCurrency)
	t.CostEstimated = t.CostEstimated || rec.CostEstimated
	t.InputCost += nonNegativeFloat(rec.InputCost)
	t.CachedInputCost += nonNegativeFloat(rec.CachedInputCost)
	t.CacheCreationInputCost += nonNegativeFloat(rec.CacheCreationInputCost)
	t.OutputCost += nonNegativeFloat(rec.OutputCost)
	t.TotalCost += nonNegativeFloat(rec.TotalCost)
	if len(rec.CacheDetails) > 0 {
		if t.CacheDetails == nil {
			t.CacheDetails = map[string]int64{}
		}
		for key, value := range rec.CacheDetails {
			t.CacheDetails[key] += nonNegative(value)
		}
	}
}

func normalizeRequestRecord(rec RequestRecord) RequestRecord {
	rec.TS = strings.TrimSpace(rec.TS)
	if rec.TS == "" {
		rec.TS = time.Now().UTC().Format(time.RFC3339)
	}
	rec.RunID = strings.TrimSpace(rec.RunID)
	rec.OriginEventID = strings.TrimSpace(rec.OriginEventID)
	rec.Provider = normalizeProvider(rec.Provider)
	rec.APIBase = normalizeAPIBase(rec.APIBase)
	rec.APIHost = normalizeAPIHost(rec.APIHost, rec.APIBase, rec.Provider)
	rec.Model = normalizeModel(rec.Model)
	rec.Operation = normalizeOperation(rec.Operation)
	rec.Scene = normalizeScene(rec.Scene)
	rec.InputTokens = nonNegative(rec.InputTokens)
	rec.OutputTokens = nonNegative(rec.OutputTokens)
	rec.TotalTokens = nonNegative(rec.TotalTokens)
	rec.CachedInputTokens = nonNegative(rec.CachedInputTokens)
	rec.CacheCreationInputTokens = nonNegative(rec.CacheCreationInputTokens)
	rec.CacheDetails = normalizeCacheDetails(rec.CacheDetails)
	rec.CostCurrency = normalizeCostCurrency(rec.CostCurrency)
	rec.InputCost = nonNegativeFloat(rec.InputCost)
	rec.CachedInputCost = nonNegativeFloat(rec.CachedInputCost)
	rec.CacheCreationInputCost = nonNegativeFloat(rec.CacheCreationInputCost)
	rec.OutputCost = nonNegativeFloat(rec.OutputCost)
	rec.TotalCost = nonNegativeFloat(rec.TotalCost)
	if rec.TotalTokens == 0 {
		rec.TotalTokens = rec.InputTokens + rec.OutputTokens
	}
	if rec.DurationMs < 0 {
		rec.DurationMs = 0
	}
	return rec
}

func validateRequestRecord(rec RequestRecord) error {
	if strings.TrimSpace(rec.TS) == "" {
		return fmt.Errorf("ts is required")
	}
	if strings.TrimSpace(rec.APIHost) == "" {
		return fmt.Errorf("api_host is required")
	}
	if strings.TrimSpace(rec.Model) == "" {
		return fmt.Errorf("model is required")
	}
	return nil
}

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return unknownProvider
	}
	return provider
}

func normalizeModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return unknownModel
	}
	return model
}

func normalizeScene(scene string) string {
	scene = strings.TrimSpace(scene)
	if scene == "" {
		return unknownScene
	}
	return scene
}

func normalizeOperation(operation string) string {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return operationChat
	}
	return operation
}

func normalizeAPIBase(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	u.Fragment = ""
	if u.Path == "/" {
		u.Path = ""
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func normalizeAPIHost(rawHost, apiBase, provider string) string {
	host := strings.ToLower(strings.TrimSpace(rawHost))
	if host != "" {
		return host
	}
	if parsed := hostFromURL(apiBase); parsed != "" {
		return parsed
	}
	return "provider:" + normalizeProvider(provider)
}

func hostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return ""
	}
	return host
}

func nonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func nonNegativeFloat(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func normalizeCacheDetails(in map[string]int64) map[string]int64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int64, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = nonNegative(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeCostCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

func mergeCostCurrency(current, next string) string {
	current = normalizeCostCurrency(current)
	next = normalizeCostCurrency(next)
	if current == "" {
		return next
	}
	if next == "" || current == next {
		return current
	}
	return "MIXED"
}

func sortModelSummaries(items []ModelSummary) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].TotalTokens != items[j].TotalTokens {
			return items[i].TotalTokens > items[j].TotalTokens
		}
		if items[i].Requests != items[j].Requests {
			return items[i].Requests > items[j].Requests
		}
		return strings.ToLower(items[i].Model) < strings.ToLower(items[j].Model)
	})
}

func sortAPIHostSummaries(items []APIHostSummary) {
	for i := range items {
		sortModelSummaries(items[i].Models)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TotalTokens != items[j].TotalTokens {
			return items[i].TotalTokens > items[j].TotalTokens
		}
		if items[i].Requests != items[j].Requests {
			return items[i].Requests > items[j].Requests
		}
		return items[i].APIHost < items[j].APIHost
	})
}
