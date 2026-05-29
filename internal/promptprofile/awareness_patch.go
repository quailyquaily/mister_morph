package promptprofile

import (
	_ "embed"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
)

//go:embed prompts/system.awareness.md
var awarenessPromptPatchSource string

func AppendAwarenessPromptPatch(spec *agent.PromptSpec) {
	if spec == nil {
		return
	}
	content := strings.TrimSpace(awarenessPromptPatchSource)
	if content == "" {
		return
	}
	spec.Blocks = append(spec.Blocks, agent.PromptBlock{Content: content})
}
