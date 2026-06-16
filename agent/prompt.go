package agent

import (
	"strings"

	"github.com/quailyquaily/mistermorph/tools"
)

type PromptSpec struct {
	Identity          string
	Rules             []string
	Skills            []PromptSkill
	Blocks            []PromptBlock
	FinalOnlyResponse bool
}

type PromptBlock struct {
	Content string
}

type PromptSkill struct {
	Name         string
	FilePath     string
	Description  string
	AuthProfiles []string
}

func DefaultPromptSpec() PromptSpec {
	spec := PromptSpec{
		Identity: "You are MisterMorph, a general-purpose AI agent that can use tools to complete tasks.",
	}
	if content := strings.TrimSpace(selfObservationBlockSource); content != "" {
		spec.Blocks = append(spec.Blocks, PromptBlock{Content: content})
	}
	return spec
}

func BuildSystemPrompt(registry *tools.Registry, spec PromptSpec) string {
	rendered, err := renderSystemPrompt(registry, spec)
	if err == nil && strings.TrimSpace(rendered) != "" {
		return rendered
	}
	return ""
}
