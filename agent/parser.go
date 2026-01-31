package agent

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/quailyquaily/mister_morph/llm"
)

var (
	ErrParseFailure    = errors.New("failed to parse agent response from LLM output")
	ErrInvalidToolCall = errors.New("tool_call response missing tool name")
	ErrInvalidPlan     = errors.New("plan response missing payload")
	ErrInvalidFinal    = errors.New("final response missing payload")
)

var codeBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)\\n\\s*```")

func ParseResponse(result llm.Result) (*AgentResponse, error) {
	var lastErr error

	if result.JSON != nil {
		data, err := json.Marshal(result.JSON)
		if err == nil {
			resp, err := unmarshalAndValidate(data)
			if err == nil {
				return resp, nil
			}
			lastErr = err
		}
	}

	text := strings.TrimSpace(result.Text)
	if text == "" {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ErrParseFailure
	}

	if resp, err := tryParse(text); err == nil {
		return resp, nil
	} else {
		lastErr = err
	}

	if jsonStr := extractFromCodeBlock(text); jsonStr != "" {
		resp, err := unmarshalAndValidate([]byte(jsonStr))
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	if jsonStr := extractJSONObject(text); jsonStr != "" {
		resp, err := unmarshalAndValidate([]byte(jsonStr))
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrParseFailure
}

func tryParse(text string) (*AgentResponse, error) {
	return unmarshalAndValidate([]byte(text))
}

func unmarshalAndValidate(data []byte) (*AgentResponse, error) {
	var resp AgentResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	if resp.Type == TypeFinal || resp.Type == TypeFinalAnswer {
		var raw struct {
			Final       json.RawMessage `json:"final,omitempty"`
			FinalAnswer json.RawMessage `json:"final_answer,omitempty"`
		}
		if json.Unmarshal(data, &raw) == nil {
			if len(raw.FinalAnswer) > 0 {
				resp.RawFinalAnswer = raw.FinalAnswer
			} else {
				resp.RawFinalAnswer = raw.Final
			}
		}
	}

	return validate(&resp)
}

func validate(resp *AgentResponse) (*AgentResponse, error) {
	switch resp.Type {
	case TypeToolCall:
		if resp.ToolCall == nil || resp.ToolCall.Name == "" {
			return nil, ErrInvalidToolCall
		}
	case TypePlan:
		if resp.PlanPayload() == nil {
			return nil, ErrInvalidPlan
		}
	case TypeFinal, TypeFinalAnswer:
		if resp.FinalPayload() == nil {
			return nil, ErrInvalidFinal
		}
	default:
		return nil, ErrParseFailure
	}
	return resp, nil
}

func extractFromCodeBlock(text string) string {
	matches := codeBlockRe.FindStringSubmatch(text)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func extractJSONObject(text string) string {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		c := text[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}
