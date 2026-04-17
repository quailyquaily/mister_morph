package promptprofile

import (
	_ "embed"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
)

//go:embed prompts/system.openai.gpt_5.md
var gpt5PromptPatchSource string

const gpt5PromptPatchFileName = "system.openai.gpt_5.md"

func AppendGPT5PromptPatch(spec *agent.PromptSpec, model string, log *slog.Logger) {
	if spec == nil || !isGPT5FamilyModel(model) {
		return
	}
	content := strings.TrimSpace(gpt5PromptPatchSource)
	if content == "" {
		return
	}
	spec.Blocks = append(spec.Blocks, agent.PromptBlock{Content: content})
}

func isGPT5FamilyModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx+1 < len(model) {
		model = model[idx+1:]
	}
	if model == "gpt-5" {
		return true
	}
	return strings.HasPrefix(model, "gpt-5.") || strings.HasPrefix(model, "gpt-5-")
}
