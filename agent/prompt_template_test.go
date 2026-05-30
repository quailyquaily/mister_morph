package agent

import (
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

func TestBuildSystemPrompt_SkillItemsUseAuthProfilesWithoutRequirements(t *testing.T) {
	prompt := BuildSystemPrompt(nil, PromptSpec{
		Identity: "identity",
		Skills: []PromptSkill{
			{
				Name:         "jsonbill",
				FilePath:     "file_state_dir/skills/jsonbill/SKILL.md",
				Description:  "Generate invoice PDF.",
				AuthProfiles: []string{"jsonbill"},
			},
			{
				Name:        "alpha",
				FilePath:    "file_state_dir/skills/alpha/SKILL.md",
				Description: "No auth needed.",
			},
		},
	})

	if !strings.Contains(prompt, "auth_profiles: jsonbill") {
		t.Fatalf("prompt missing auth profiles line: %s", prompt)
	}
	if strings.Contains(prompt, "Requirements:") {
		t.Fatalf("prompt should not contain Requirements: %s", prompt)
	}
	if strings.Contains(prompt, "auth_profiles: \n") {
		t.Fatalf("prompt should not emit empty AuthProfiles lines: %s", prompt)
	}
	if strings.Contains(prompt, "[[ TODO Workflow ]]") {
		t.Fatalf("prompt should not include todo workflow without an injected block: %s", prompt)
	}
}

func TestBuildSystemPrompt_DoesNotInjectPlatformSection(t *testing.T) {
	prompt := BuildSystemPrompt(nil, DefaultPromptSpec())

	if strings.Contains(prompt, "Platform Information") {
		t.Fatalf("prompt should not include platform section: %s", prompt)
	}
	if strings.Contains(prompt, "Available shell tool") {
		t.Fatalf("prompt should not include shell tool section: %s", prompt)
	}
}

func TestBuildSystemPrompt_FinalOnlyResponseOmitsPlanAndResponseRules(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "plan_create"})

	prompt := BuildSystemPrompt(reg, PromptSpec{
		Identity:          "identity",
		FinalOnlyResponse: true,
	})

	if strings.Contains(prompt, "Option 1: Plan") {
		t.Fatalf("prompt should not include plan response option: %s", prompt)
	}
	if strings.Contains(prompt, "## Response Rules") {
		t.Fatalf("prompt should not include response rules: %s", prompt)
	}
	if !strings.Contains(prompt, `"type": "final"`) {
		t.Fatalf("prompt missing final JSON contract: %s", prompt)
	}
}

func TestAvailableShellToolName_OnlyReturnsSingleRegisteredShell(t *testing.T) {
	reg := tools.NewRegistry()
	if got := availableShellToolName(reg); got != "" {
		t.Fatalf("availableShellToolName() = %q, want empty", got)
	}

	reg.Register(&mockTool{name: "bash"})
	if got := availableShellToolName(reg); got != "bash" {
		t.Fatalf("availableShellToolName() = %q, want bash", got)
	}

	reg.Register(&mockTool{name: "powershell"})
	if got := availableShellToolName(reg); got != "" {
		t.Fatalf("availableShellToolName() = %q, want empty when both shells exist", got)
	}

	gotNames := availableShellToolNames(reg)
	if strings.Join(gotNames, ",") != "bash,powershell" {
		t.Fatalf("availableShellToolNames() = %v, want [bash powershell]", gotNames)
	}
}
