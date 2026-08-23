package chatcmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/clifmt"
)

// formatRawChatOutput returns the raw assistant output without any terminal
// formatting or ANSI codes. It is intended for use in LLM history.
func formatRawChatOutput(final *agent.Final) string {
	if final == nil {
		return ""
	}
	switch output := final.Output.(type) {
	case string:
		return strings.TrimSpace(output)
	case nil:
		payload, _ := json.MarshalIndent(final, "", "  ")
		return strings.TrimSpace(string(payload))
	default:
		payload, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(output))
		}
		return strings.TrimSpace(string(payload))
	}
}

// formatChatOutput returns the terminal-rendered version of the assistant
// output, including Markdown/ANSI formatting for display.
func formatChatOutput(final *agent.Final) string {
	if final == nil {
		return ""
	}
	switch output := final.Output.(type) {
	case string:
		return clifmt.RenderMarkdown(strings.TrimSpace(output))
	case nil:
		payload, _ := json.MarshalIndent(final, "", "  ")
		return strings.TrimSpace(string(payload))
	default:
		payload, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(output))
		}
		return strings.TrimSpace(string(payload))
	}
}

func stripMarkdownFences(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```markdown") {
		content = strings.TrimPrefix(content, "```markdown")
		content = strings.TrimSpace(content)
		if strings.HasSuffix(content, "```") {
			content = strings.TrimSuffix(content, "```")
			content = strings.TrimSpace(content)
		}
		return content
	}
	if strings.HasPrefix(content, "```") {
		idx := strings.Index(content, "\n")
		if idx > 0 {
			content = content[idx+1:]
		} else {
			content = strings.TrimPrefix(content, "```")
		}
		content = strings.TrimSpace(content)
		if strings.HasSuffix(content, "```") {
			content = strings.TrimSuffix(content, "```")
			content = strings.TrimSpace(content)
		}
		return content
	}
	return content
}
