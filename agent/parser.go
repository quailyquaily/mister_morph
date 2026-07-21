package agent

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/llm"
)

var (
	ErrParseFailure    = errors.New("failed to parse agent response from LLM output")
	ErrInvalidToolCall = errors.New("tool_call JSON responses are not supported")
	ErrInvalidPlan     = errors.New("plan response missing payload")
	ErrInvalidFinal    = errors.New("final response missing payload")
)

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

	if candidates, err := jsonutil.FindJSONCandidates(text); err == nil {
		for _, data := range candidates {
			resp, err := unmarshalAndValidate(data)
			if err == nil {
				return resp, nil
			}
			lastErr = err
		}
	} else {
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrParseFailure
}

func unmarshalAndValidate(data []byte) (*AgentResponse, error) {
	var resp AgentResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	switch resp.Type {
	case TypePlan:
		var plan Plan
		if err := json.Unmarshal(data, &plan); err != nil {
			return nil, err
		}
		resp.Plan = &plan
	case TypeFinal, TypeFinalAnswer:
		var final Final
		if err := json.Unmarshal(data, &final); err != nil {
			return nil, err
		}
		if resp.Type == TypeFinalAnswer {
			resp.FinalAnswer = &final
		} else {
			resp.Final = &final
		}
		if raw, err := rawResponsePayload(data); err == nil {
			resp.RawFinalAnswer = raw
		}
	}

	return validate(&resp)
}

func rawResponsePayload(data []byte) (json.RawMessage, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	delete(payload, "type")
	return json.Marshal(payload)
}

func validate(resp *AgentResponse) (*AgentResponse, error) {
	switch resp.Type {
	case TypeToolCall:
		return nil, ErrInvalidToolCall
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
