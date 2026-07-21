package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
)

func (e *Engine) finalEgress(ctx context.Context, st *engineLoopState, step int, final *Final, raw json.RawMessage) (*Final, error) {
	if st == nil || st.agentCtx == nil {
		return nil, fmt.Errorf("nil engine state")
	}
	if final == nil {
		return nil, fmt.Errorf("nil final output")
	}

	if e.guard == nil || !e.guard.Enabled() {
		st.agentCtx.RawFinalAnswer = append(json.RawMessage(nil), raw...)
		return final, nil
	}

	payload := make(map[string]any)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decode raw final output: %w", err)
		}
	}
	encodedFinal, err := json.Marshal(final)
	if err != nil {
		return nil, fmt.Errorf("encode final output: %w", err)
	}
	var finalFields map[string]any
	if err := json.Unmarshal(encodedFinal, &finalFields); err != nil {
		return nil, fmt.Errorf("decode final output: %w", err)
	}
	for key, value := range finalFields {
		payload[key] = value
	}

	guardedPayload, err := e.guardOutputValue(ctx, st, step, payload)
	if err != nil {
		return nil, err
	}
	payload, ok := guardedPayload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("guard returned invalid final output")
	}

	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode guarded final output: %w", err)
	}
	var guardedFinal Final
	if err := json.Unmarshal(encodedPayload, &guardedFinal); err != nil {
		return nil, fmt.Errorf("decode guarded final output: %w", err)
	}
	if final.Plan != nil {
		st.agentCtx.Plan = guardedFinal.Plan
	}
	if len(raw) > 0 {
		st.agentCtx.RawFinalAnswer = append(json.RawMessage(nil), encodedPayload...)
	} else {
		st.agentCtx.RawFinalAnswer = nil
	}
	return &guardedFinal, nil
}

func (e *Engine) guardPlanForPublish(ctx context.Context, st *engineLoopState, step int, plan *Plan) (*Plan, error) {
	if plan == nil || e.guard == nil || !e.guard.Enabled() {
		return plan, nil
	}
	encoded, err := json.Marshal(map[string]any{"plan": plan})
	if err != nil {
		return nil, fmt.Errorf("encode plan output: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, fmt.Errorf("decode plan output: %w", err)
	}
	guardedPayload, err := e.guardOutputValue(ctx, st, step, payload)
	if err != nil {
		return nil, err
	}
	encoded, err = json.Marshal(guardedPayload)
	if err != nil {
		return nil, fmt.Errorf("encode guarded plan output: %w", err)
	}
	var guarded struct {
		Plan *Plan `json:"plan"`
	}
	if err := json.Unmarshal(encoded, &guarded); err != nil {
		return nil, fmt.Errorf("decode guarded plan output: %w", err)
	}
	if guarded.Plan == nil {
		return nil, fmt.Errorf("guard returned invalid plan output")
	}
	return guarded.Plan, nil
}

func (e *Engine) guardOutputValue(ctx context.Context, st *engineLoopState, step int, value any) (any, error) {
	if e.guard == nil || !e.guard.Enabled() {
		return value, nil
	}
	result, err := e.guard.Evaluate(ctx, guard.Meta{
		RunID: st.runID,
		Step:  step,
		Time:  time.Now().UTC(),
	}, guard.Action{
		Type:  guard.ActionOutputPublish,
		Value: value,
	})
	if err != nil {
		return nil, fmt.Errorf("evaluate output: %w", err)
	}
	switch result.Decision {
	case guard.DecisionAllow:
		return value, nil
	case guard.DecisionAllowWithRedact:
		return result.RedactedValue, nil
	case guard.DecisionDeny:
		return nil, fmt.Errorf("output blocked by guard")
	default:
		return nil, fmt.Errorf("unsupported output guard decision: %s", result.Decision)
	}
}
