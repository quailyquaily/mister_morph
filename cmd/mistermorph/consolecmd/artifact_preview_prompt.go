package consolecmd

import (
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/prompttmpl"
)

const consoleArtifactPreviewPromptTemplateSource = "## Console Artifact Preview\n\n" +
	"An `artifact` fenced block works as an inline preview.\n" +
	"When you create or update a previewable static web result (like a HTML file), include exactly one artifact block in the final answer.\n\n" +
	"Only include it after the HTML entry file exists. Use this shape as raw Markdown in the final answer:\n\n" +
	"```artifact\n" +
	"path=path/to/profile.html\n" +
	"dir_name={{.DirNameExample}}\n" +
	"```\n\n" +
	"Rules:\n" +
	"- `path` is the relative path to an `.html` or `.htm` entry file.\n" +
	"- `dir_name` selects one allowed root: {{.DirNameList}}.\n" +
	"- Use the actual HTML path you created.\n" +
	"- Do not overwrite an existing preview file unless the user asked for replacement; choose a non-conflicting descriptive filename instead.\n" +
	"- Do not emit an artifact block for server-backed apps, external URLs, or files you did not create or verify.\n"

var consoleArtifactPreviewPromptTemplate = prompttmpl.MustParse(
	"console_artifact_preview_prompt",
	consoleArtifactPreviewPromptTemplateSource,
	nil,
)

type consoleArtifactPreviewPromptData struct {
	DirNameExample string
	DirNameList    string
}

func consoleArtifactPreviewPromptBlock(workspaceDir string) (agent.PromptBlock, error) {
	dirNames := consoleArtifactPreviewDirNames(workspaceDir)
	if len(dirNames) == 0 {
		return agent.PromptBlock{}, nil
	}
	content, err := prompttmpl.Render(consoleArtifactPreviewPromptTemplate, consoleArtifactPreviewPromptData{
		DirNameExample: strings.Join(dirNames, "|"),
		DirNameList:    "`" + strings.Join(dirNames, "`, `") + "`",
	})
	if err != nil {
		return agent.PromptBlock{}, err
	}
	return agent.PromptBlock{
		Content: strings.TrimSpace(content),
	}, nil
}

func consoleArtifactPreviewDirNames(workspaceDir string) []string {
	dirNames := make([]string, 0, 3)
	if strings.TrimSpace(workspaceDir) != "" {
		dirNames = append(dirNames, "workspace_dir")
	}
	dirNames = append(dirNames, "file_cache_dir", "file_state_dir")
	return dirNames
}
