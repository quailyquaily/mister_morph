package chatcmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
)

func formatChatOutput(final *agent.Final) string {
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

func formatPlanProgressUpdate(runCtx *agent.Context, update agent.PlanStepUpdate) string {
	if runCtx == nil || runCtx.Plan == nil {
		return ""
	}
	if update.CompletedIndex < 0 && update.StartedIndex < 0 {
		return ""
	}
	total := len(runCtx.Plan.Steps)
	if total == 0 {
		return ""
	}

	if update.CompletedIndex >= 0 && update.CompletedIndex == total-1 && update.StartedIndex < 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("plan: ")

	if update.CompletedIndex >= 0 && update.CompletedStep != "" {
		b.WriteString(fmt.Sprintf("✓ %s", update.CompletedStep))
	}

	if update.StartedIndex >= 0 && update.StartedStep != "" {
		if update.CompletedIndex >= 0 {
			b.WriteString(" → ")
		}
		b.WriteString(update.StartedStep)
	}

	if update.CompletedIndex >= 0 {
		b.WriteString(fmt.Sprintf(" [%d/%d]", update.CompletedIndex+1, total))
	} else if update.StartedIndex >= 0 {
		b.WriteString(fmt.Sprintf(" [%d/%d]", update.StartedIndex+1, total))
	}

	return b.String()
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

func truncateString(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

func stringDisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeDisplayWidth(r)
	}
	return w
}

func runeDisplayWidth(r rune) int {
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if r >= 0x1100 &&
		(r <= 0x115f || r == 0x2329 || r == 0x232a || (r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
			(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) ||
			(r >= 0xfe10 && r <= 0xfe19) || (r >= 0xfe30 && r <= 0xfe6f) ||
			(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) ||
			(r >= 0x20000 && r <= 0x2fffd) || (r >= 0x30000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}
