package uniai

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/goutils/structs"
	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
	uniaichat "github.com/quailyquaily/uniai/chat"
)

type Config struct {
	Provider string
	Endpoint string
	APIKey   string
	Model    string
	Headers  map[string]string
	Pricing  *uniaiapi.PricingCatalog

	RequestTimeout  time.Duration
	Temperature     *float64
	ReasoningEffort string
	ReasoningBudget *int
	CacheTTL        string
	CacheKeyPrefix  string

	ToolsEmulationMode  string
	AzureAPIKey         string
	AzureEndpoint       string
	AzureDeployment     string
	AwsKey              string
	AwsSecret           string
	AwsRegion           string
	AwsBedrockModelArn  string
	CloudflareAccountID string
	CloudflareAPIToken  string
	CloudflareAPIBase   string

	Debug bool
}

type Client struct {
	provider           string
	model              string
	pricing            *uniaiapi.PricingCatalog
	requestTimeout     time.Duration
	temperature        *float64
	reasoningEffort    string
	reasoningBudget    *int
	cacheTTL           string
	cacheKeyPrefix     string
	toolsEmulationMode uniaiapi.ToolsEmulationMode
	client             *uniaiapi.Client
}

func New(cfg Config) *Client {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	pricing := cfg.Pricing
	if pricing == nil {
		pricing = uniaiapi.DefaultPricingCatalog()
	}

	openAIBase := normalizeOpenAIBase(cfg.Endpoint)
	openAIKey := strings.TrimSpace(cfg.APIKey)

	azureAPIKey := firstNonEmpty(cfg.AzureAPIKey, cfg.APIKey)
	azureEndpoint := firstNonEmpty(cfg.AzureEndpoint, cfg.Endpoint)
	azureDeployment := firstNonEmpty(cfg.AzureDeployment, cfg.Model)

	anthropicKey := strings.TrimSpace(cfg.APIKey)
	anthropicModel := strings.TrimSpace(cfg.Model)

	geminiKey := strings.TrimSpace(cfg.APIKey)
	geminiBase := strings.TrimSpace(cfg.Endpoint)

	uCfg := uniaiapi.Config{
		Provider:            provider,
		OpenAIAPIKey:        openAIKey,
		OpenAIAPIBase:       openAIBase,
		OpenAIModel:         strings.TrimSpace(cfg.Model),
		ChatHeaders:         cloneStringMap(cfg.Headers),
		AzureOpenAIAPIKey:   strings.TrimSpace(azureAPIKey),
		AzureOpenAIEndpoint: strings.TrimSpace(azureEndpoint),
		AzureOpenAIModel:    strings.TrimSpace(azureDeployment),
		AnthropicAPIKey:     strings.TrimSpace(anthropicKey),
		AnthropicModel:      strings.TrimSpace(anthropicModel),
		AwsKey:              strings.TrimSpace(cfg.AwsKey),
		AwsSecret:           strings.TrimSpace(cfg.AwsSecret),
		AwsRegion:           strings.TrimSpace(cfg.AwsRegion),
		AwsBedrockModelArn:  strings.TrimSpace(cfg.AwsBedrockModelArn),
		CloudflareAccountID: strings.TrimSpace(cfg.CloudflareAccountID),
		CloudflareAPIToken:  strings.TrimSpace(cfg.CloudflareAPIToken),
		CloudflareAPIBase:   strings.TrimSpace(cfg.CloudflareAPIBase),
		GeminiAPIKey:        strings.TrimSpace(geminiKey),
		GeminiAPIBase:       strings.TrimSpace(geminiBase),
		Pricing:             pricing,

		Debug: cfg.Debug,
	}

	return &Client{
		provider:           provider,
		model:              strings.TrimSpace(cfg.Model),
		pricing:            pricing,
		requestTimeout:     cfg.RequestTimeout,
		temperature:        cloneFloat64(cfg.Temperature),
		reasoningEffort:    strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort)),
		reasoningBudget:    cloneInt(cfg.ReasoningBudget),
		cacheTTL:           strings.TrimSpace(cfg.CacheTTL),
		cacheKeyPrefix:     strings.TrimSpace(cfg.CacheKeyPrefix),
		toolsEmulationMode: normalizeToolsEmulationMode(cfg.ToolsEmulationMode),
		client:             uniaiapi.New(uCfg),
	}
}

func (c *Client) Chat(ctx context.Context, req llm.Request) (llm.Result, error) {
	start := time.Now()
	if c.requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		defer cancel()
	}
	streamDebug := newStreamDebugCapture(c.provider, req.DebugFn, req.OnStream != nil && supportsStreaming(c.provider))
	if streamDebug != nil {
		req.OnStream = streamDebug.Wrap(req.OnStream)
	}
	opts := buildChatOptions(req, c.provider, c.model, c.cacheTTL, c.cacheKeyPrefix, req.ForceJSON, c.toolsEmulationMode, c.temperature, c.reasoningEffort, c.reasoningBudget)
	resp, err := c.client.Chat(ctx, opts...)
	if err != nil {
		streamDebug.EmitPartial(err)
		c.emitChatError(req.DebugFn, err, req.ForceJSON, 1)
	}
	if err != nil && req.ForceJSON && shouldRetryWithoutResponseFormat(err) {
		streamDebug.Reset()
		opts = buildChatOptions(req, c.provider, c.model, c.cacheTTL, c.cacheKeyPrefix, false, c.toolsEmulationMode, c.temperature, c.reasoningEffort, c.reasoningBudget)
		resp, err = c.client.Chat(ctx, opts...)
		if err != nil {
			streamDebug.EmitPartial(err)
			c.emitChatError(req.DebugFn, err, false, 2)
		}
	}
	if err != nil {
		return llm.Result{}, err
	}
	if resp == nil {
		err = fmt.Errorf("uniai: empty response")
		c.emitChatError(req.DebugFn, err, req.ForceJSON, 0)
		return llm.Result{}, err
	}
	streamDebug.EmitResponse(resp)

	toolCalls := toLLMToolCalls(resp.ToolCalls)
	model := firstNonEmpty(req.Model, c.model)
	usage := toLLMUsage(resp.Usage)
	if enriched, changed := enrichUsageFromOpenAICompatibleRaw(usage, resp.Raw); changed {
		usage = recalculateUsageCost(enriched, c.pricing, req.InferenceProvider, model)
	}
	if shouldEnsureGeminiThoughtSignature(c.provider, model) {
		toolCalls = ensureGeminiToolCallThoughtSignatures(toolCalls)
	}

	return llm.Result{
		Text:      resp.Text,
		Parts:     toLLMParts(resp.Parts),
		ToolCalls: toolCalls,
		Usage:     usage,
		Duration:  time.Since(start),
	}, nil
}

func shouldEnsureGeminiThoughtSignature(provider, _ string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "gemini")
}

func buildChatOptions(req llm.Request, provider string, defaultModel string, cacheTTL string, cacheKeyPrefix string, forceJSON bool, toolsEmulationMode uniaiapi.ToolsEmulationMode, defaultTemperature *float64, defaultReasoningEffort string, defaultReasoningBudget *int) []uniaiapi.ChatOption {
	req = adaptRequestForProvider(req, provider)
	msgs := make([]uniaiapi.Message, len(req.Messages))
	for i, m := range req.Messages {
		msg := uniaiapi.Message{Role: m.Role, Content: m.Content}
		if len(m.Parts) > 0 {
			msg.Parts = toUniaiPartsFromLLM(provider, m.Parts)
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
				if ctrl, ok := toUniaiCacheControlForProvider(provider, *t.CacheControl); ok {
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

	applyPromptCacheOptions(provider, firstNonEmpty(req.Model, defaultModel), cacheTTL, cacheKeyPrefix, req, openAIOptions, azureOptions)
	if forceJSON && len(req.Tools) == 0 {
		openAIOptions["response_format"] = "json_object"
		if strings.EqualFold(strings.TrimSpace(provider), "azure") {
			azureOptions["response_format"] = "json_object"
		}
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
	if req.OnStream != nil && supportsStreaming(provider) {
		opts = append(opts, uniaiapi.WithOnStream(func(ev uniaiapi.StreamEvent) error {
			streamEvent := llm.StreamEvent{
				Delta: ev.Delta,
				Done:  ev.Done,
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
				if enriched, changed := enrichUsageFromOpenAICompatibleRaw(usage, streamEventRaw(ev)); changed {
					usage = enriched
				}
				streamEvent.Usage = &usage
			}
			return req.OnStream(streamEvent)
		}))
	}

	return opts
}

func supportsStreaming(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "gemini", "cloudflare":
		return false
	default:
		return true
	}
}

func cloneFloat64(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

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

type rawJSONProvider interface {
	RawJSON() string
}

type openAICompatibleUsagePayload struct {
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
	if err := json.Unmarshal([]byte(rawJSON), &response); err == nil && response.Usage.hasCacheUsage() {
		return response.Usage, true
	}
	var usage openAICompatibleUsagePayload
	if err := json.Unmarshal([]byte(rawJSON), &usage); err != nil || !usage.hasCacheUsage() {
		return openAICompatibleUsagePayload{}, false
	}
	return usage, true
}

func (p openAICompatibleUsagePayload) hasCacheUsage() bool {
	return p.CachedTokens != nil ||
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
		if value != nil && *value > 0 {
			return *value
		}
	}
	return 0
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

func toLLMCacheControl(ctrl *uniaiapi.CacheControl) *llm.CacheControl {
	if ctrl == nil {
		return nil
	}
	return &llm.CacheControl{TTL: strings.TrimSpace(ctrl.TTL)}
}

func toUniaiCacheControlForProvider(provider string, ctrl llm.CacheControl) (uniaiapi.CacheControl, bool) {
	ttl := explicitCacheTTLForProvider(provider, ctrl.TTL)
	if ttl == "" {
		return uniaiapi.CacheControl{}, false
	}
	return uniaiapi.CacheControl{TTL: ttl}, true
}

func adaptRequestForProvider(req llm.Request, provider string) llm.Request {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic":
		return req
	case "bedrock":
		return stripExplicitCacheControl(req, false, true)
	default:
		return stripExplicitCacheControl(req, true, true)
	}
}

func stripExplicitCacheControl(req llm.Request, stripAllParts bool, stripTools bool) llm.Request {
	out := req

	if len(req.Messages) > 0 {
		messages := make([]llm.Message, len(req.Messages))
		copy(messages, req.Messages)
		changed := false
		for i, msg := range messages {
			if len(msg.Parts) == 0 {
				continue
			}
			parts := make([]llm.Part, len(msg.Parts))
			copy(parts, msg.Parts)
			partChanged := false
			for j, part := range parts {
				if part.CacheControl == nil {
					continue
				}
				if stripAllParts || strings.EqualFold(strings.TrimSpace(msg.Role), "system") {
					part.CacheControl = nil
					parts[j] = part
					partChanged = true
				}
			}
			if partChanged {
				msg.Parts = parts
				messages[i] = msg
				changed = true
			}
		}
		if changed {
			out.Messages = messages
		}
	}

	if stripTools && len(req.Tools) > 0 {
		tools := make([]llm.Tool, len(req.Tools))
		copy(tools, req.Tools)
		changed := false
		for i, tool := range tools {
			if tool.CacheControl == nil {
				continue
			}
			tool.CacheControl = nil
			tools[i] = tool
			changed = true
		}
		if changed {
			out.Tools = tools
		}
	}

	return out
}

func applyPromptCacheOptions(provider, model, cacheTTL, cacheKeyPrefix string, req llm.Request, openAIOptions, azureOptions structs.JSONMap) {
	retention := promptCacheRetentionForProvider(provider, cacheTTL)
	key := derivedPromptCacheKey(provider, model, cacheKeyPrefix, req)
	if key == "" && retention == "" {
		return
	}
	var target structs.JSONMap
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "openai_resp":
		target = openAIOptions
	case "azure":
		target = azureOptions
	default:
		return
	}
	if key != "" {
		target["prompt_cache_key"] = key
	}
	if retention != "" {
		target["prompt_cache_retention"] = retention
	}
}

func normalizeToolsEmulationMode(mode string) uniaiapi.ToolsEmulationMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "force":
		return uniaiapi.ToolsEmulationForce
	case "fallback":
		return uniaiapi.ToolsEmulationFallback
	default:
		return uniaiapi.ToolsEmulationOff
	}
}

func (c *Client) emitChatError(debugFn func(label, payload string), err error, forceJSON bool, attempt int) {
	if err == nil || c == nil || debugFn == nil {
		return
	}

	provider := strings.TrimSpace(c.provider)
	if provider == "" {
		provider = "openai"
	}
	label := provider + ".chat.error"

	payload := map[string]any{
		"provider": provider,
		"error":    err.Error(),
	}
	if attempt > 0 {
		payload["attempt"] = attempt
	}
	if forceJSON {
		payload["force_json"] = true
	}

	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		debugFn(label, err.Error())
		return
	}
	debugFn(label, string(data))
}

type streamDebugCapture struct {
	provider string
	debugFn  func(label, payload string)

	text       strings.Builder
	events     int
	deltaBytes int
	done       bool
	usage      *llm.Usage
	toolCalls  map[int]*streamDebugToolCall
	toolOrder  []int
}

type streamDebugToolCall struct {
	Index     int    `json:"index"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	ArgsChunk string `json:"args_chunk,omitempty"`
}

func newStreamDebugCapture(provider string, debugFn func(label, payload string), enabled bool) *streamDebugCapture {
	if !enabled || debugFn == nil {
		return nil
	}
	return &streamDebugCapture{
		provider:  normalizedDebugProvider(provider),
		debugFn:   debugFn,
		toolCalls: map[int]*streamDebugToolCall{},
	}
}

func (s *streamDebugCapture) Wrap(next llm.StreamHandler) llm.StreamHandler {
	if s == nil || next == nil {
		return next
	}
	return func(event llm.StreamEvent) error {
		s.observe(event)
		return next(event)
	}
}

func (s *streamDebugCapture) observe(event llm.StreamEvent) {
	if s == nil {
		return
	}
	if event.Delta == "" && event.ToolCallDelta == nil && event.Usage == nil && !event.Done {
		return
	}
	s.events++
	if event.Delta != "" {
		s.deltaBytes += len(event.Delta)
		_, _ = s.text.WriteString(event.Delta)
	}
	if event.ToolCallDelta != nil {
		s.observeToolCall(*event.ToolCallDelta)
	}
	if event.Usage != nil {
		usage := *event.Usage
		s.usage = &usage
	}
	if event.Done {
		s.done = true
	}
}

func (s *streamDebugCapture) observeToolCall(delta llm.StreamToolCallDelta) {
	if s == nil {
		return
	}
	call, ok := s.toolCalls[delta.Index]
	if !ok {
		call = &streamDebugToolCall{Index: delta.Index}
		s.toolCalls[delta.Index] = call
		s.toolOrder = append(s.toolOrder, delta.Index)
	}
	if strings.TrimSpace(delta.ID) != "" {
		call.ID = delta.ID
	}
	if strings.TrimSpace(delta.Name) != "" {
		call.Name = delta.Name
	}
	if delta.ArgsChunk != "" {
		call.ArgsChunk += delta.ArgsChunk
	}
}

func (s *streamDebugCapture) Reset() {
	if s == nil {
		return
	}
	s.text.Reset()
	s.events = 0
	s.deltaBytes = 0
	s.done = false
	s.usage = nil
	s.toolCalls = map[int]*streamDebugToolCall{}
	s.toolOrder = nil
}

func (s *streamDebugCapture) EmitPartial(err error) {
	if s == nil || s.debugFn == nil || err == nil {
		return
	}
	payload := map[string]any{
		"provider":    s.provider,
		"error":       err.Error(),
		"events":      s.events,
		"delta_bytes": s.deltaBytes,
	}
	if s.done {
		payload["done"] = true
	}
	if text := s.text.String(); text != "" {
		payload["text"] = text
	}
	if toolCalls := s.snapshotToolCalls(); len(toolCalls) > 0 {
		payload["tool_calls"] = toolCalls
	}
	if s.usage != nil {
		payload["usage"] = s.usage
	}
	s.debugFn(chatStreamPartialDebugLabel(s.provider), marshalDebugPayload(payload))
}

func (s *streamDebugCapture) EmitResponse(resp *uniaichat.Result) {
	if s == nil || s.debugFn == nil || resp == nil {
		return
	}
	s.debugFn(chatResponseDebugLabel(s.provider), chatResultDebugPayload(resp))
}

func (s *streamDebugCapture) snapshotToolCalls() []streamDebugToolCall {
	if s == nil || len(s.toolOrder) == 0 {
		return nil
	}
	out := make([]streamDebugToolCall, 0, len(s.toolOrder))
	for _, index := range s.toolOrder {
		if call := s.toolCalls[index]; call != nil {
			out = append(out, *call)
		}
	}
	return out
}

func chatResultDebugPayload(resp *uniaichat.Result) string {
	if resp == nil {
		return "null"
	}
	if raw := rawJSONText(resp.Raw); raw != "" {
		return raw
	}
	return marshalDebugPayload(resp)
}

func rawJSONText(value any) string {
	if value == nil {
		return ""
	}
	type rawJSONer interface {
		RawJSON() string
	}
	if v, ok := value.(rawJSONer); ok {
		if raw := strings.TrimSpace(v.RawJSON()); raw != "" {
			return raw
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		return ""
	}
	return raw
}

func marshalDebugPayload(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(data)
}

func normalizedDebugProvider(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "openai"
	}
	return provider
}

func chatResponseDebugLabel(provider string) string {
	provider = normalizedDebugProvider(provider)
	if strings.EqualFold(provider, "openai_resp") {
		return "openai.responses.response"
	}
	return provider + ".chat.response"
}

func chatStreamPartialDebugLabel(provider string) string {
	provider = normalizedDebugProvider(provider)
	if strings.EqualFold(provider, "openai_resp") {
		return "openai.responses.stream.partial"
	}
	return provider + ".chat.stream.partial"
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

func toUniaiPartsFromLLM(provider string, parts []llm.Part) []uniaiapi.Part {
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
				ctrl, ok := toUniaiCacheControlForProvider(provider, *part.CacheControl)
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

func promptCacheRetentionForProvider(provider, rawTTL string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "openai_resp", "azure":
	default:
		return ""
	}
	return normalizePromptCacheRetention(rawTTL)
}

func normalizePromptCacheRetention(rawTTL string) string {
	rawTTL = strings.TrimSpace(rawTTL)
	if rawTTL == "" || strings.EqualFold(rawTTL, "off") {
		return ""
	}
	switch strings.ToLower(rawTTL) {
	case "short":
		return "in_memory"
	case "long":
		return "24h"
	}
	d, err := time.ParseDuration(rawTTL)
	if err != nil {
		return ""
	}
	if d <= 5*time.Minute {
		return "in_memory"
	}
	return "24h"
}

func explicitCacheTTLForProvider(provider, rawTTL string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "bedrock":
	default:
		return ""
	}
	rawTTL = strings.TrimSpace(rawTTL)
	if rawTTL == "" || strings.EqualFold(rawTTL, "off") {
		return ""
	}
	switch strings.ToLower(rawTTL) {
	case "short":
		return "5m"
	case "long":
		return "1h"
	}
	d, err := time.ParseDuration(rawTTL)
	if err != nil {
		return ""
	}
	if d <= 5*time.Minute {
		return "5m"
	}
	return "1h"
}

func derivedPromptCacheKey(provider, model, cacheKeyPrefix string, req llm.Request) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "openai_resp", "azure":
	default:
		return ""
	}
	cacheKeyPrefix = strings.TrimSpace(cacheKeyPrefix)

	stable := promptCacheStablePayload{
		Model: strings.TrimSpace(model),
		Scene: strings.TrimSpace(req.Scene),
	}
	for _, msg := range req.Messages {
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "system") {
			continue
		}
		stable.Messages = append(stable.Messages, stablePromptMessage{
			Content: strings.TrimSpace(msg.Content),
			Parts:   stableParts(msg.Parts),
		})
	}
	for _, tool := range req.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		stable.Tools = append(stable.Tools, stablePromptTool{
			Name:           name,
			Description:    strings.TrimSpace(tool.Description),
			ParametersJSON: strings.TrimSpace(tool.ParametersJSON),
		})
	}
	if len(stable.Messages) == 0 && len(stable.Tools) == 0 {
		return cacheKeyPrefix
	}
	data, err := json.Marshal(stable)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	key := "mm-" + base64.RawURLEncoding.EncodeToString(sum[:12])
	if cacheKeyPrefix == "" {
		return key
	}
	return cacheKeyPrefix + "-" + key
}

type promptCacheStablePayload struct {
	Model    string                `json:"model,omitempty"`
	Scene    string                `json:"scene,omitempty"`
	Messages []stablePromptMessage `json:"messages,omitempty"`
	Tools    []stablePromptTool    `json:"tools,omitempty"`
}

type stablePromptMessage struct {
	Content string       `json:"content,omitempty"`
	Parts   []stablePart `json:"parts,omitempty"`
}

type stablePromptTool struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	ParametersJSON string `json:"parameters_json,omitempty"`
}

type stablePart struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	URL        string `json:"url,omitempty"`
	DataBase64 string `json:"data_base64,omitempty"`
	MIMEType   string `json:"mime_type,omitempty"`
}

func stableParts(parts []llm.Part) []stablePart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]stablePart, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type)
		if partType == "" {
			continue
		}
		out = append(out, stablePart{
			Type:       partType,
			Text:       strings.TrimSpace(part.Text),
			URL:        strings.TrimSpace(part.URL),
			DataBase64: strings.TrimSpace(part.DataBase64),
			MIMEType:   strings.TrimSpace(part.MIMEType),
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

func shouldRetryWithoutResponseFormat(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "response_format") || strings.Contains(msg, "response format")
}

func normalizeOpenAIBase(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	endpoint = strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(endpoint, "/v1") || strings.Contains(endpoint, "/v1/") {
		return endpoint
	}
	return endpoint + "/v1"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func floatFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if val, err := v.Float64(); err == nil {
			return val, true
		}
	case string:
		if val, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return val, true
		}
	}
	return 0, false
}

func intFromAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if val, err := v.Int64(); err == nil {
			return int(val), true
		}
	case string:
		if val, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return val, true
		}
	}
	return 0, false
}

func stringSliceFromAny(value any) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return append([]string{}, v...), true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, false
		}
		return []string{strings.TrimSpace(v)}, true
	default:
		return nil, false
	}
}
