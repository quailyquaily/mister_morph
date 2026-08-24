package chatcmd

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/clifmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/tools/builtin"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

const (
	chatToolOutputMaxLines      = 5
	chatToolOutputFallbackWidth = 120
)

func configureChatSessionCallbacks(sess *chatSession, logger *slog.Logger) {
	if sess == nil {
		return
	}
	sess.onPlanStepUpdate = func(runCtx *agent.Context, update agent.PlanStepUpdate) {
		if logger != nil {
			logger.Debug("plan_step_update_callback", "completedIndex", update.CompletedIndex, "startedIndex", update.StartedIndex, "startedStep", update.StartedStep, "reason", update.Reason)
		}
		total := 0
		if runCtx != nil && runCtx.Plan != nil {
			total = len(runCtx.Plan.Steps)
		}
		if update.Reason == "plan_created" && runCtx != nil && runCtx.Plan != nil {
			if output := formatChatPlan(runCtx.Plan); output != "" {
				_, _ = fmt.Fprintln(sess.currentWriter(), output)
			}
		}
		if update.StartedIndex >= 0 && strings.TrimSpace(update.StartedStep) != "" {
			sess.setActivity(formatChatPlanActivity(update.StartedIndex, total, update.StartedStep), false)
			return
		}
		if total > 0 && update.CompletedIndex+1 >= total {
			_, _ = fmt.Fprintf(sess.currentWriter(), "%s Plan complete · %d steps\n", chatSuccessStyle.Render("✓"), total)
			sess.setActivity("waiting for model", false)
		}
	}

	sess.onToolCallStart = func(_ *agent.Context, call agent.ToolCall) {
		sess.setActivity(formatChatToolActivity(call), true)
		if call.Name != "write_file" {
			return
		}
		path, _ := call.Params["path"].(string)
		_, resolvedPath, err := builtin.ResolveWritePath(pathroots.New(sess.workspaceDir, sess.fileCacheDir, sess.fileStateDir), path)
		if err != nil {
			return
		}
		data, err := os.ReadFile(resolvedPath)
		if err == nil {
			sess.fileSnapshots[resolvedPath] = string(data)
		}
	}

	sess.onToolCallDone = func(_ *agent.Context, call agent.ToolCall, observation string, callErr error) {
		width := chatTranscriptWidth(sess)
		toolLines := strings.Split(formatChatToolTranscript(call, width), "\n")
		for index, line := range toolLines {
			toolLines[index] = chatSecondaryStyle.Render(line)
		}
		toolCall := strings.Join(toolLines, "\n")
		if callErr != nil {
			observation = strings.TrimSuffix(observation, "\n\nerror: "+callErr.Error())
		}
		outputLines := formatChatToolOutput(call, observation, width)
		for index, line := range outputLines {
			outputLines[index] = chatMutedStyle.Render(line)
		}
		writer := sess.currentWriter()
		if callErr != nil {
			errorLines := indentChatLines([]string{"error: " + escapeTerminalControls(strings.TrimSpace(callErr.Error()))}, width)
			_, _ = fmt.Fprintf(writer, "%s %s\n", chatErrorStyle.Render("×"), toolCall)
			if len(outputLines) > 0 {
				_, _ = fmt.Fprintln(writer, strings.Join(outputLines, "\n"))
			}
			_, _ = fmt.Fprintf(writer, "%s\n\n", strings.Join(errorLines, "\n"))
			sess.setActivity("waiting for model", false)
			return
		}
		if call.Name == "plan_create" {
			return
		}
		_, _ = fmt.Fprintf(writer, "%s %s\n", chatSuccessStyle.Render("✓"), toolCall)
		if len(outputLines) > 0 {
			_, _ = fmt.Fprintln(writer, strings.Join(outputLines, "\n"))
		}
		_, _ = fmt.Fprintln(writer)
		sess.setActivity("waiting for model", false)
		if call.Name != "write_file" {
			return
		}

		path, _ := call.Params["path"].(string)
		_, resolvedPath, err := builtin.ResolveWritePath(pathroots.New(sess.workspaceDir, sess.fileCacheDir, sess.fileStateDir), path)
		if err != nil {
			return
		}
		oldContent, hadOld := sess.fileSnapshots[resolvedPath]
		delete(sess.fileSnapshots, resolvedPath)
		newData, err := os.ReadFile(resolvedPath)
		if err != nil {
			return
		}
		newContent := string(newData)
		if hadOld && oldContent == newContent {
			return
		}
		if diff := clifmt.RenderDiff(resolvedPath, oldContent, newContent); diff != "" {
			_, _ = fmt.Fprintln(writer, strings.TrimPrefix(diff, "\n"))
		}
	}
}

func formatChatToolActivity(call agent.ToolCall) string {
	parts := []string{chatToolName(call)}
	inline, block := formatChatToolParams(call.Params)
	for _, line := range append(inline, block...) {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " · ")
}

func formatChatToolTranscript(call agent.ToolCall, width int) string {
	inline, block := formatChatToolParams(call.Params)
	summary := chatToolName(call)
	if len(inline) > 0 {
		summary += " · " + strings.Join(inline, " · ")
	}
	lines := wrapChatToolSummary(summary, width)
	lines = append(lines, indentChatLines(block, width)...)
	return strings.Join(lines, "\n")
}

func wrapChatToolSummary(summary string, width int) []string {
	if width <= 2 {
		return []string{summary}
	}
	wrapped := strings.Split(ansi.Hardwrap(summary, width-2, false), "\n")
	for index := 1; index < len(wrapped); index++ {
		wrapped[index] = "  " + wrapped[index]
	}
	return wrapped
}

func chatToolName(call agent.ToolCall) string {
	if name := strings.TrimSpace(call.Name); name != "" {
		return name
	}
	return "tool"
}

func formatChatToolParamLines(params map[string]any) []string {
	inline, block := formatChatToolParams(params)
	return append(inline, block...)
}

func formatChatToolParams(params map[string]any) ([]string, []string) {
	if len(params) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	inline := make([]string, 0, len(keys))
	block := make([]string, 0)
	for _, key := range keys {
		displayKey := strings.ReplaceAll(escapeTerminalControls(key), "\n", `\n`)
		valueLines := formatChatToolValueLines(params[key])
		if len(valueLines) == 1 {
			inline = append(inline, displayKey+": "+valueLines[0])
			continue
		}
		block = append(block, displayKey+":")
		for _, line := range valueLines {
			block = append(block, "  "+line)
		}
	}
	return inline, block
}

func formatChatToolValueLines(value any) []string {
	if text, ok := value.(string); ok {
		text = escapeTerminalControls(text)
		if text == "" {
			return []string{`""`}
		}
		return strings.Split(text, "\n")
	}
	payload, err := yaml.Marshal(value)
	if err != nil {
		return []string{escapeTerminalControls(fmt.Sprint(value))}
	}
	return strings.Split(escapeTerminalControls(strings.TrimSuffix(string(payload), "\n")), "\n")
}

func indentChatLines(lines []string, width int) []string {
	if len(lines) == 0 {
		return nil
	}
	contentWidth := 0
	if width > 2 {
		contentWidth = width - 2
	}
	indented := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped := line
		if contentWidth > 0 {
			wrapped = ansi.Hardwrap(line, contentWidth, false)
		}
		for _, segment := range strings.Split(wrapped, "\n") {
			indented = append(indented, "  "+segment)
		}
	}
	return indented
}

func formatChatToolOutput(call agent.ToolCall, observation string, width int) []string {
	name := strings.ToLower(strings.TrimSpace(call.Name))
	if name == "plan_create" || name == "write_file" {
		return nil
	}
	if name == "bash" || name == "powershell" {
		if shellOutput, ok := chatShellOutput(observation); ok {
			observation = shellOutput
		}
	}
	observation = strings.TrimSpace(escapeTerminalControls(observation))
	if observation == "" {
		return nil
	}

	contentWidth := chatToolOutputFallbackWidth - 4
	if width > 4 {
		contentWidth = width - 4
	}
	visible := make([]string, 0, chatToolOutputMaxLines+1)
	for _, line := range strings.Split(observation, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		for _, wrapped := range strings.Split(ansi.Hardwrap(line, contentWidth, false), "\n") {
			visible = append(visible, wrapped)
		}
	}
	if len(visible) == 0 {
		return nil
	}
	if len(visible) > chatToolOutputMaxLines {
		omitted := len(visible) - 4
		omission := fmt.Sprintf("… %d lines omitted", omitted)
		if ansi.StringWidth(omission) > contentWidth {
			omission = fmt.Sprintf("… %d omitted", omitted)
		}
		if ansi.StringWidth(omission) > contentWidth {
			omission = "…"
		}
		visible = []string{
			visible[0],
			visible[1],
			omission,
			visible[len(visible)-2],
			visible[len(visible)-1],
		}
	}
	for index := range visible {
		prefix := "    "
		if index == 0 {
			prefix = "  └ "
		}
		visible[index] = prefix + visible[index]
	}
	return visible
}

func chatShellOutput(observation string) (string, bool) {
	header, streams, ok := strings.Cut(observation, "\nstdout:\n")
	if !ok || !strings.HasPrefix(header, "exit_code: ") {
		return "", false
	}
	stdout, stderr, ok := strings.Cut(streams, "\n\nstderr:\n")
	if !ok {
		return "", false
	}

	parts := make([]string, 0, 3)
	if stdout = strings.TrimSpace(stdout); stdout != "" {
		parts = append(parts, stdout)
	}
	if stderr = strings.TrimSpace(stderr); stderr != "" {
		stderrLines := strings.Split(stderr, "\n")
		parts = append(parts, "stderr: "+stderrLines[0])
		parts = append(parts, stderrLines[1:]...)
	}
	if strings.Contains(header, "stdout_truncated: true") || strings.Contains(header, "stderr_truncated: true") {
		parts = append(parts, "… shell output truncated")
	}
	return strings.Join(parts, "\n"), true
}

func chatTranscriptWidth(sess *chatSession) int {
	if sess == nil || sess.cmd == nil {
		return 0
	}
	file, ok := sess.cmd.OutOrStdout().(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return 0
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width <= 1 {
		return 0
	}
	return width - 1
}

func formatChatPlanActivity(index, total int, step string) string {
	prefix := "plan"
	if total > 0 {
		prefix = fmt.Sprintf("plan %d/%d", index+1, total)
	}
	return prefix + " · " + escapeTerminalControls(normalizeActivityText(step))
}

func formatChatPlan(plan *agent.Plan) string {
	if plan == nil || len(plan.Steps) == 0 {
		return ""
	}
	lines := make([]string, 1, len(plan.Steps)+1)
	lines[0] = "Plan"
	for i, step := range plan.Steps {
		lines = append(lines, fmt.Sprintf("  %d. %s", i+1, escapeTerminalControls(strings.TrimSpace(step.Step))))
	}
	return strings.Join(lines, "\n")
}
