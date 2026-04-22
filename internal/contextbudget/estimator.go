package contextbudget

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/tiktoken-go/tokenizer"
)

const (
	requestBaseOverheadTokens = 24
	messageBaseOverheadTokens = 8
	partBaseOverheadTokens    = 4
	toolBaseOverheadTokens    = 16
	forceJSONOverheadTokens   = 16
	toolChoiceOverheadTokens  = 12
	emulationOverheadTokens   = 512
	imageBaseOverheadTokens   = 1024
	externalImageURLTokens    = 256
)

type Estimator struct {
	provider           string
	model              string
	toolsEmulationMode string
}

var (
	codecsOnce  sync.Once
	codecsErr   error
	cl100kCodec tokenizer.Codec
	o200kCodec  tokenizer.Codec
)

func NewEstimator(provider string, model string, toolsEmulationMode string) (*Estimator, error) {
	if err := loadCodecs(); err != nil {
		return nil, err
	}
	return &Estimator{
		provider:           normalizeProvider(provider),
		model:              strings.TrimSpace(model),
		toolsEmulationMode: strings.ToLower(strings.TrimSpace(toolsEmulationMode)),
	}, nil
}

func (e *Estimator) EstimateRequest(req llm.Request) (int, error) {
	if e == nil {
		return 0, fmt.Errorf("token estimator is nil")
	}
	req = adaptRequestForProvider(req, e.provider)
	total := requestBaseOverheadTokens
	for _, msg := range req.Messages {
		n, err := e.estimateMessage(msg)
		if err != nil {
			return 0, err
		}
		total += n
	}
	for _, tool := range req.Tools {
		n, err := e.estimateTool(tool)
		if err != nil {
			return 0, err
		}
		total += n
	}
	if req.ForceJSON && len(req.Tools) == 0 {
		total += forceJSONOverheadTokens
	}
	if len(req.Tools) > 0 {
		total += toolChoiceOverheadTokens
		if e.toolsEmulationMode != "" && e.toolsEmulationMode != "off" {
			total += emulationOverheadTokens
		}
	}
	return total, nil
}

func (e *Estimator) EstimateMessages(messages []llm.Message) (int, error) {
	if e == nil {
		return 0, fmt.Errorf("token estimator is nil")
	}
	total := 0
	for _, msg := range messages {
		n, err := e.estimateMessage(msg)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func (e *Estimator) estimateMessage(msg llm.Message) (int, error) {
	total := messageBaseOverheadTokens
	total += e.estimateText(strings.TrimSpace(msg.Role))
	total += e.estimateText(strings.TrimSpace(msg.Content))
	if strings.TrimSpace(msg.ToolCallID) != "" {
		total += e.estimateText(strings.TrimSpace(msg.ToolCallID)) + partBaseOverheadTokens
	}
	for _, part := range msg.Parts {
		n, err := e.estimatePart(part)
		if err != nil {
			return 0, err
		}
		total += n
	}
	for _, call := range msg.ToolCalls {
		n, err := e.estimateToolCall(call)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func (e *Estimator) estimatePart(part llm.Part) (int, error) {
	switch strings.ToLower(strings.TrimSpace(part.Type)) {
	case llm.PartTypeImageURL:
		return partBaseOverheadTokens + estimateImageURLTokens(part.URL), nil
	case llm.PartTypeImageBase64:
		return partBaseOverheadTokens + estimateImageBytesTokens(encodedBytesLen(part.DataBase64)), nil
	default:
		return partBaseOverheadTokens + e.estimateText(strings.TrimSpace(part.Text)), nil
	}
}

func (e *Estimator) estimateTool(tool llm.Tool) (int, error) {
	total := toolBaseOverheadTokens
	total += e.estimateText(strings.TrimSpace(tool.Name))
	total += e.estimateText(strings.TrimSpace(tool.Description))
	total += e.estimateText(strings.TrimSpace(tool.ParametersJSON))
	return total, nil
}

func (e *Estimator) estimateToolCall(call llm.ToolCall) (int, error) {
	total := toolBaseOverheadTokens
	total += e.estimateText(strings.TrimSpace(call.ID))
	total += e.estimateText(strings.TrimSpace(call.Type))
	total += e.estimateText(strings.TrimSpace(call.Name))
	total += e.estimateText(strings.TrimSpace(call.RawArguments))
	total += e.estimateText(strings.TrimSpace(call.ThoughtSignature))
	if len(call.Arguments) > 0 {
		data, err := json.Marshal(call.Arguments)
		if err != nil {
			return 0, err
		}
		total += e.estimateText(string(data))
	}
	return total, nil
}

func (e *Estimator) estimateText(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	maxTokens := 0
	if cl100kCodec != nil {
		if n, err := cl100kCodec.Count(text); err == nil && n > maxTokens {
			maxTokens = n
		}
	}
	if o200kCodec != nil {
		if n, err := o200kCodec.Count(text); err == nil && n > maxTokens {
			maxTokens = n
		}
	}
	if maxTokens > 0 {
		return maxTokens
	}
	// Conservative fallback when local tokenizer unexpectedly fails.
	return int(math.Ceil(float64(len(text)) / 3.0))
}

func loadCodecs() error {
	codecsOnce.Do(func() {
		cl100kCodec, codecsErr = tokenizer.Get(tokenizer.Cl100kBase)
		if codecsErr != nil {
			return
		}
		o200kCodec, codecsErr = tokenizer.Get(tokenizer.O200kBase)
	})
	return codecsErr
}

func adaptRequestForProvider(req llm.Request, provider string) llm.Request {
	switch provider {
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

func estimateImageURLTokens(url string) int {
	url = strings.TrimSpace(url)
	if url == "" {
		return imageBaseOverheadTokens
	}
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		comma := strings.IndexByte(url, ',')
		if comma < 0 || comma+1 >= len(url) {
			return imageBaseOverheadTokens
		}
		return estimateImageBytesTokens(encodedBytesLen(url[comma+1:]))
	}
	return externalImageURLTokens
}

func estimateImageBytesTokens(sizeBytes int) int {
	if sizeBytes <= 0 {
		return imageBaseOverheadTokens
	}
	// V1 keeps this deliberately simple and conservative: use a fixed floor
	// and add roughly 1 token per 512 bytes for larger payloads.
	tokens := int(math.Ceil(float64(sizeBytes) / 512.0))
	if tokens < imageBaseOverheadTokens {
		return imageBaseOverheadTokens
	}
	return tokens
}

func encodedBytesLen(data string) int {
	data = strings.TrimSpace(data)
	if data == "" {
		return 0
	}
	decodedLen := base64.StdEncoding.DecodedLen(len(data))
	if decodedLen > 0 {
		return decodedLen
	}
	return int(math.Ceil(float64(len(data)) * 0.75))
}
