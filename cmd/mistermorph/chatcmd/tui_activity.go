package chatcmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/clifmt"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/tools/builtin"
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
			sess.setActivity(formatChatPlanActivity(update.StartedIndex, total, update.StartedStep))
			return
		}
		if total > 0 && update.CompletedIndex+1 >= total {
			_, _ = fmt.Fprintf(sess.currentWriter(), "%s Plan complete · %d steps\n", chatSuccessStyle.Render("✓"), total)
			sess.setActivity("waiting for model")
		}
	}

	sess.onToolCallStart = func(_ *agent.Context, call agent.ToolCall) {
		sess.setActivity(formatChatToolActivity(call))
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

	sess.onToolCallDone = func(_ *agent.Context, call agent.ToolCall, _ string, callErr error) {
		activity := formatChatToolActivity(call)
		writer := sess.currentWriter()
		if callErr != nil {
			_, _ = fmt.Fprintf(writer, "%s %s\n  %s\n", chatErrorStyle.Render("×"), activity, normalizeActivityText(callErr.Error()))
			sess.setActivity("waiting for model")
			return
		}
		if call.Name == "plan_create" {
			return
		}
		_, _ = fmt.Fprintf(writer, "%s %s\n", chatSuccessStyle.Render("✓"), activity)
		sess.setActivity("waiting for model")
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
			_, _ = fmt.Fprintln(writer, diff)
		}
	}
}

func formatChatToolActivity(call agent.ToolCall) string {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		name = "tool"
	}
	if len(call.Params) == 0 {
		return name
	}
	payload, err := json.Marshal(call.Params)
	if err != nil {
		return name + " · " + fmt.Sprint(call.Params)
	}
	return name + " · " + string(payload)
}

func formatChatPlanActivity(index, total int, step string) string {
	prefix := "plan"
	if total > 0 {
		prefix = fmt.Sprintf("plan %d/%d", index+1, total)
	}
	return prefix + " · " + normalizeActivityText(step)
}

func formatChatPlan(plan *agent.Plan) string {
	if plan == nil || len(plan.Steps) == 0 {
		return ""
	}
	lines := make([]string, 1, len(plan.Steps)+1)
	lines[0] = "Plan"
	for i, step := range plan.Steps {
		lines = append(lines, fmt.Sprintf("  %d. %s", i+1, strings.TrimSpace(step.Step)))
	}
	return strings.Join(lines, "\n")
}
