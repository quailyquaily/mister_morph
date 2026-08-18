package consolecmd

import (
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
)

func TestBuildConsolePlanProgressSkipsBlankSteps(t *testing.T) {
	progress := buildConsolePlanProgress(&agent.Plan{
		Steps: []agent.PlanStep{
			{Step: "  scan repo  ", Status: agent.PlanStatusCompleted},
			{Step: "   ", Status: agent.PlanStatusPending},
			{Step: "patch bug", Status: agent.PlanStatusInProgress},
		},
	})
	if progress == nil {
		t.Fatal("progress = nil")
	}
	if len(progress.Steps) != 2 {
		t.Fatalf("len(progress.Steps) = %d, want 2", len(progress.Steps))
	}
	if progress.Steps[0].Step != "scan repo" {
		t.Fatalf("progress.Steps[0].Step = %q, want %q", progress.Steps[0].Step, "scan repo")
	}
	if progress.Steps[0].Status != agent.PlanStatusCompleted {
		t.Fatalf("progress.Steps[0].Status = %q, want %q", progress.Steps[0].Status, agent.PlanStatusCompleted)
	}
	if progress.Steps[1].Step != "patch bug" {
		t.Fatalf("progress.Steps[1].Step = %q, want %q", progress.Steps[1].Step, "patch bug")
	}
	if progress.Steps[1].Status != agent.PlanStatusInProgress {
		t.Fatalf("progress.Steps[1].Status = %q, want %q", progress.Steps[1].Status, agent.PlanStatusInProgress)
	}
}
