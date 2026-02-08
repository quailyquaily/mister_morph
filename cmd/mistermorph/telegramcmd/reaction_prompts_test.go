package telegramcmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
)

func TestRenderReactionCategoryPrompts(t *testing.T) {
	intent := agent.Intent{
		Goal:        "Acknowledge thanks",
		Deliverable: "Short acknowledgement",
		Constraints: []string{"keep short"},
	}
	sys, user, err := renderReactionCategoryPrompts(intent, "thanks!")
	if err != nil {
		t.Fatalf("renderReactionCategoryPrompts() error = %v", err)
	}
	if !strings.Contains(sys, "classify reaction category") {
		t.Fatalf("unexpected system prompt: %q", sys)
	}

	var payload struct {
		Intent struct {
			Goal        string   `json:"goal"`
			Deliverable string   `json:"deliverable"`
			Constraints []string `json:"constraints"`
		} `json:"intent"`
		Task       string   `json:"task"`
		Categories []string `json:"categories"`
		Rules      []string `json:"rules"`
	}
	if err := json.Unmarshal([]byte(user), &payload); err != nil {
		t.Fatalf("user prompt is not valid json: %v", err)
	}
	if payload.Intent.Goal != intent.Goal {
		t.Fatalf("intent.goal = %q, want %q", payload.Intent.Goal, intent.Goal)
	}
	if payload.Task != "thanks!" {
		t.Fatalf("task = %q, want %q", payload.Task, "thanks!")
	}
	if len(payload.Categories) == 0 {
		t.Fatalf("categories is empty")
	}
	if len(payload.Rules) == 0 {
		t.Fatalf("rules is empty")
	}
}
