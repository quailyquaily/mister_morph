package uniai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/llm"
	uniaichat "github.com/quailyquaily/uniai/chat"
)

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
	if strings.EqualFold(provider, "openai_resp") || strings.EqualFold(provider, "openai_codex") {
		return "openai.responses.response"
	}
	return provider + ".chat.response"
}

func chatStreamPartialDebugLabel(provider string) string {
	provider = normalizedDebugProvider(provider)
	if strings.EqualFold(provider, "openai_resp") || strings.EqualFold(provider, "openai_codex") {
		return "openai.responses.stream.partial"
	}
	return provider + ".chat.stream.partial"
}
