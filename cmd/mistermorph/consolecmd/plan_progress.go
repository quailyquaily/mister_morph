package consolecmd

import (
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
)

type consolePlanProgress struct {
	Steps []consolePlanStep `json:"steps,omitempty"`
}

type consolePlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status,omitempty"`
}

func cloneConsolePlanProgress(progress *consolePlanProgress) *consolePlanProgress {
	if progress == nil {
		return nil
	}
	out := &consolePlanProgress{
		Steps: make([]consolePlanStep, len(progress.Steps)),
	}
	copy(out.Steps, progress.Steps)
	return out
}

func buildConsolePlanProgress(plan *agent.Plan) *consolePlanProgress {
	if plan == nil {
		return nil
	}
	steps := make([]consolePlanStep, 0, len(plan.Steps))
	for _, raw := range plan.Steps {
		step := strings.TrimSpace(raw.Step)
		if step == "" {
			continue
		}
		steps = append(steps, consolePlanStep{
			Step:   step,
			Status: strings.TrimSpace(raw.Status),
		})
	}
	if len(steps) == 0 {
		return nil
	}
	return &consolePlanProgress{Steps: steps}
}

func consoleTaskPlan(final *agent.Final, runCtx *agent.Context) *agent.Plan {
	if runCtx != nil && runCtx.Plan != nil {
		return runCtx.Plan
	}
	if final != nil && final.Plan != nil {
		return final.Plan
	}
	return nil
}
