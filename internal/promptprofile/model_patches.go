package promptprofile

import (
	_ "embed"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/llm"
)

//go:embed prompts/system.openai.gpt_5.md
var gpt5PromptPatchSource string

//go:embed prompts/system.qwen_3.md
var qwen3PromptPatchSource string

func AppendModelPromptPatches(spec *agent.PromptSpec, model string) {
	AppendGPT5PromptPatch(spec, model)
	appendQwen3PromptPatch(spec, model)
}

func AppendGPT5PromptPatch(spec *agent.PromptSpec, model string) {
	if spec == nil || !isGPT5FamilyModel(model) {
		return
	}
	content := strings.TrimSpace(gpt5PromptPatchSource)
	if content == "" {
		return
	}
	spec.Blocks = append(spec.Blocks, agent.PromptBlock{Content: content})
}

func appendQwen3PromptPatch(spec *agent.PromptSpec, model string) {
	if spec == nil || !isQwen3FamilyModel(model) {
		return
	}
	content := strings.TrimSpace(qwen3PromptPatchSource)
	if content == "" {
		return
	}
	spec.Blocks = append(spec.Blocks, agent.PromptBlock{Content: content})
}

func isGPT5FamilyModel(model string) bool {
	model = strings.ToLower(llm.ShortModelName(model))
	if model == "" {
		return false
	}
	if model == "gpt-5.5" ||
		strings.HasPrefix(model, "gpt-5.5.") ||
		strings.HasPrefix(model, "gpt-5.5-") ||
		strings.HasPrefix(model, "gpt-5.5:") {
		return false
	}
	if model == "gpt-5" {
		return true
	}
	return strings.HasPrefix(model, "gpt-5.") || strings.HasPrefix(model, "gpt-5-")
}

func isQwen3FamilyModel(model string) bool {
	model = strings.ToLower(llm.ShortModelName(model))
	if model == "" {
		return false
	}
	if model == "qwen3" || model == "qwen-3" {
		return true
	}
	return strings.HasPrefix(model, "qwen3.") ||
		strings.HasPrefix(model, "qwen3-") ||
		strings.HasPrefix(model, "qwen3:") ||
		strings.HasPrefix(model, "qwen-3.") ||
		strings.HasPrefix(model, "qwen-3-") ||
		strings.HasPrefix(model, "qwen-3:")
}
