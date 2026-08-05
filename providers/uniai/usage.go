package uniai

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
	uniaichat "github.com/quailyquaily/uniai/chat"
)

func toLLMUsage(usage uniaichat.Usage) llm.Usage {
	return llm.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		Cache:        toLLMUsageCache(usage.Cache),
		Cost:         toLLMUsageCost(usage.Cost),
	}
}

func toLLMUsageCache(cache uniaichat.UsageCache) llm.UsageCache {
	return llm.UsageCache{
		CachedInputTokens:        cache.CachedInputTokens,
		CacheCreationInputTokens: cache.CacheCreationInputTokens,
		Details:                  cloneIntMap(cache.Details),
	}
}

func toLLMUsageCost(cost *uniaichat.UsageCost) *llm.UsageCost {
	if cost == nil {
		return nil
	}
	return &llm.UsageCost{
		Currency:           cost.Currency,
		Estimated:          cost.Estimated,
		Input:              cost.Input,
		CachedInput:        cost.CachedInput,
		CacheCreationInput: cost.CacheCreationInput,
		Output:             cost.Output,
		Total:              cost.Total,
	}
}

func providerUsesOpenAICompatibleUsage(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "openai_custom", "openai_resp", "openai_codex", "azure", "deepseek", "xai", "meta":
		return true
	default:
		return false
	}
}

type rawJSONProvider interface {
	RawJSON() string
}

type openAICompatibleUsagePayload struct {
	PromptTokens             *int           `json:"prompt_tokens"`
	CompletionTokens         *int           `json:"completion_tokens"`
	TotalTokens              *int           `json:"total_tokens"`
	InputTokens              *int           `json:"input_tokens"`
	OutputTokens             *int           `json:"output_tokens"`
	CachedTokens             *int           `json:"cached_tokens"`
	CacheReadInputTokens     *int           `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int           `json:"cache_creation_input_tokens"`
	CacheCreation            map[string]int `json:"cache_creation"`
	PromptTokensDetails      struct {
		CachedTokens             *int           `json:"cached_tokens"`
		CacheReadInputTokens     *int           `json:"cache_read_input_tokens"`
		CacheCreationInputTokens *int           `json:"cache_creation_input_tokens"`
		CacheCreation            map[string]int `json:"cache_creation"`
	} `json:"prompt_tokens_details"`
}

func enrichUsageFromOpenAICompatibleRaw(usage llm.Usage, raw any) (llm.Usage, bool) {
	changed := false
	for _, rawJSON := range rawJSONCandidatesFromOpenAICompatibleRaw(raw) {
		payload, ok := parseOpenAICompatibleUsagePayload(rawJSON)
		if !ok {
			continue
		}
		var payloadChanged bool
		usage, payloadChanged = applyOpenAICompatibleUsagePayload(usage, payload)
		changed = changed || payloadChanged
	}
	return usage, changed
}

func applyOpenAICompatibleUsagePayload(usage llm.Usage, payload openAICompatibleUsagePayload) (llm.Usage, bool) {
	changed := false
	if input := maxPositiveInt(payload.PromptTokens, payload.InputTokens); input > 0 && input > usage.InputTokens {
		usage.InputTokens = input
		changed = true
	}
	if output := maxPositiveInt(payload.CompletionTokens, payload.OutputTokens); output > 0 && output > usage.OutputTokens {
		usage.OutputTokens = output
		changed = true
	}
	if total := positiveIntValue(payload.TotalTokens); total > 0 && total > usage.TotalTokens {
		usage.TotalTokens = total
		changed = true
	}
	if cached := firstPositiveInt(
		payload.PromptTokensDetails.CacheReadInputTokens,
		payload.PromptTokensDetails.CachedTokens,
		payload.CacheReadInputTokens,
		payload.CachedTokens,
	); cached > 0 && usage.Cache.CachedInputTokens != cached {
		usage.Cache.CachedInputTokens = cached
		changed = true
	}
	if created := firstPositiveInt(
		payload.PromptTokensDetails.CacheCreationInputTokens,
		payload.CacheCreationInputTokens,
	); created > 0 && usage.Cache.CacheCreationInputTokens != created {
		usage.Cache.CacheCreationInputTokens = created
		changed = true
	}
	var detailChanged bool
	usage.Cache.Details, detailChanged = mergePositiveCacheDetails(usage.Cache.Details, payload.PromptTokensDetails.CacheCreation)
	changed = changed || detailChanged
	usage.Cache.Details, detailChanged = mergePositiveCacheDetails(usage.Cache.Details, payload.CacheCreation)
	changed = changed || detailChanged
	if usage.TotalTokens < usage.InputTokens+usage.OutputTokens {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		changed = true
	}
	return usage, changed
}

func parseOpenAICompatibleUsagePayload(rawJSON string) (openAICompatibleUsagePayload, bool) {
	rawJSON = strings.TrimSpace(rawJSON)
	if rawJSON == "" {
		return openAICompatibleUsagePayload{}, false
	}
	var response struct {
		Usage openAICompatibleUsagePayload `json:"usage"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &response); err == nil && response.Usage.hasUsageData() {
		return response.Usage, true
	}
	var usage openAICompatibleUsagePayload
	if err := json.Unmarshal([]byte(rawJSON), &usage); err != nil || !usage.hasUsageData() {
		return openAICompatibleUsagePayload{}, false
	}
	return usage, true
}

func (p openAICompatibleUsagePayload) hasUsageData() bool {
	return p.PromptTokens != nil ||
		p.CompletionTokens != nil ||
		p.TotalTokens != nil ||
		p.InputTokens != nil ||
		p.OutputTokens != nil ||
		p.CachedTokens != nil ||
		p.CacheReadInputTokens != nil ||
		p.CacheCreationInputTokens != nil ||
		len(p.CacheCreation) > 0 ||
		p.PromptTokensDetails.CachedTokens != nil ||
		p.PromptTokensDetails.CacheReadInputTokens != nil ||
		p.PromptTokensDetails.CacheCreationInputTokens != nil ||
		len(p.PromptTokensDetails.CacheCreation) > 0
}

func rawJSONCandidatesFromOpenAICompatibleRaw(raw any) []string {
	if raw == nil {
		return nil
	}
	var out []string
	out = append(out, rawJSONCandidatesFromSequence(raw)...)
	if v, ok := raw.(rawJSONProvider); ok {
		if rawJSON := strings.TrimSpace(v.RawJSON()); rawJSON != "" {
			out = append(out, rawJSON)
		}
	}
	if len(out) == 0 {
		b, err := json.Marshal(raw)
		if err == nil {
			if rawJSON := strings.TrimSpace(string(b)); rawJSON != "" {
				out = append(out, rawJSON)
			}
		}
	}
	return out
}

func rawJSONCandidatesFromSequence(raw any) []string {
	v := reflect.ValueOf(raw)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() || (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) {
		return nil
	}
	if v.Type().Elem().Kind() == reflect.Uint8 {
		return nil
	}
	out := make([]string, 0, v.Len())
	for i := v.Len() - 1; i >= 0; i-- {
		elem := v.Index(i)
		if !elem.CanInterface() {
			continue
		}
		out = append(out, rawJSONCandidatesFromOpenAICompatibleRaw(elem.Interface())...)
	}
	return out
}

func streamEventRaw(event any) any {
	v := reflect.ValueOf(event)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return nil
	}
	field := v.FieldByName("Raw")
	if !field.IsValid() || !field.CanInterface() {
		return nil
	}
	return field.Interface()
}

func firstPositiveInt(values ...*int) int {
	for _, value := range values {
		if value := positiveIntValue(value); value > 0 {
			return value
		}
	}
	return 0
}

func maxPositiveInt(values ...*int) int {
	maxValue := 0
	for _, value := range values {
		if value := positiveIntValue(value); value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

func positiveIntValue(value *int) int {
	if value == nil || *value <= 0 {
		return 0
	}
	return *value
}

func mergePositiveCacheDetails(dst map[string]int, src map[string]int) (map[string]int, bool) {
	if len(src) == 0 {
		return dst, false
	}
	changed := false
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" || value <= 0 {
			continue
		}
		if dst == nil {
			dst = map[string]int{}
		}
		if dst[key] != value {
			dst[key] = value
			changed = true
		}
	}
	return dst, changed
}

func recalculateUsageCost(usage llm.Usage, pricing *uniaiapi.PricingCatalog, inferenceProvider, model string) llm.Usage {
	if pricing == nil {
		usage.Cost = nil
		return usage
	}
	cost, ok := pricing.EstimateChatCostWithInferenceProvider(strings.TrimSpace(inferenceProvider), strings.TrimSpace(model), uniaiapi.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		Cache: uniaiapi.UsageCache{
			CachedInputTokens:        usage.Cache.CachedInputTokens,
			CacheCreationInputTokens: usage.Cache.CacheCreationInputTokens,
			Details:                  cloneIntMap(usage.Cache.Details),
		},
	})
	if !ok {
		usage.Cost = nil
		return usage
	}
	usage.Cost = toLLMUsageCost(cost)
	return usage
}

func cloneIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
