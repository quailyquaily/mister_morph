package workspace

import (
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
)

func PromptBlock(workspaceDir string) agent.PromptBlock {
	workspaceDir = strings.TrimSpace(workspaceDir)
	if workspaceDir == "" {
		return agent.PromptBlock{}
	}
	return agent.PromptBlock{
		Content: fmt.Sprintf("## Workspace Context\n\n"+
			"A local workspace is attached for this run.\n\n"+
			"workspace_dir: %s\n\n"+
			"Use this as the default working directory for project files.\n"+
			"Relative paths for read_file, write_file, bash, and powershell resolve under workspace_dir unless the user explicitly uses file_cache_dir/ or file_state_dir/.\n"+
			"Use file_cache_dir for downloads and temporary artifacts.\n"+
			"Use file_state_dir for memory, TODO, skills, contacts, and guard state.\n", workspaceDir),
	}
}
