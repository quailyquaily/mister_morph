package slack

import (
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/slackclient"
)

const (
	slackApprovalActionApprove = "morph_approval_approve"
	slackApprovalActionDeny    = "morph_approval_deny"
	slackApprovalValuePrefix   = "ap:"
)

func slackApprovalButtonValue(approvalRequestID string, approved bool) string {
	approvalRequestID = strings.TrimSpace(approvalRequestID)
	if approvalRequestID == "" {
		return ""
	}
	decision := "d"
	if approved {
		decision = "a"
	}
	return slackApprovalValuePrefix + decision + ":" + approvalRequestID
}

func parseSlackApprovalButtonValue(raw string) (approvalRequestID string, approved bool, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, slackApprovalValuePrefix) {
		return "", false, false
	}
	parts := strings.SplitN(strings.TrimPrefix(raw, slackApprovalValuePrefix), ":", 2)
	if len(parts) != 2 {
		return "", false, false
	}
	approvalRequestID = strings.TrimSpace(parts[1])
	if approvalRequestID == "" {
		return "", false, false
	}
	switch strings.TrimSpace(parts[0]) {
	case "a":
		return approvalRequestID, true, true
	case "d":
		return approvalRequestID, false, true
	default:
		return "", false, false
	}
}

func buildSlackApprovalBlocks(text string, approvalRequestID string) []slackclient.Block {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "Approval required."
	}
	approveValue := slackApprovalButtonValue(approvalRequestID, true)
	denyValue := slackApprovalButtonValue(approvalRequestID, false)
	if approveValue == "" || denyValue == "" {
		return nil
	}
	return []slackclient.Block{
		{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": text,
			},
		},
		{
			"type": "actions",
			"elements": []map[string]any{
				{
					"type":      "button",
					"text":      map[string]any{"type": "plain_text", "text": "Approve"},
					"style":     "primary",
					"action_id": slackApprovalActionApprove,
					"value":     approveValue,
				},
				{
					"type":      "button",
					"text":      map[string]any{"type": "plain_text", "text": "Deny"},
					"style":     "danger",
					"action_id": slackApprovalActionDeny,
					"value":     denyValue,
				},
			},
		},
	}
}

func slackApprovalResultText(approved bool) string {
	if approved {
		return "Approved. Resuming task."
	}
	return "Approval denied. Task canceled."
}

func slackApprovalRequestText(job slackJob, rec guard.ApprovalRecord) string {
	parts := []string{"*Approval required*"}
	if toolName := strings.TrimSpace(rec.ToolName); toolName != "" {
		parts = append(parts, "Tool: `"+escapeSlackInline(toolName)+"`")
	}
	if summary := strings.TrimSpace(rec.ActionSummaryRedacted); summary != "" {
		parts = append(parts, "Action: "+truncateSlackApprovalLine(summary, 900))
	}
	if task := strings.TrimSpace(job.Text); task != "" {
		parts = append(parts, "Task: "+truncateSlackApprovalLine(task, 700))
	}
	if id := strings.TrimSpace(rec.ID); id != "" {
		parts = append(parts, fmt.Sprintf("approval_request_id: `%s`", escapeSlackInline(id)))
	}
	return strings.Join(parts, "\n")
}

func slackApprovalActor(event slackApprovalActionEvent) string {
	if userID := strings.TrimSpace(event.UserID); userID != "" {
		return "slack:" + userID
	}
	if username := strings.TrimSpace(event.Username); username != "" {
		return "slack:@" + username
	}
	return "slack:unknown"
}

func escapeSlackInline(raw string) string {
	raw = strings.ReplaceAll(raw, "&", "&amp;")
	raw = strings.ReplaceAll(raw, "<", "&lt;")
	raw = strings.ReplaceAll(raw, ">", "&gt;")
	raw = strings.ReplaceAll(raw, "`", "'")
	return raw
}

func truncateSlackApprovalLine(raw string, max int) string {
	raw = strings.TrimSpace(raw)
	if max <= 0 || len(raw) <= max {
		return raw
	}
	return strings.TrimSpace(raw[:max]) + "..."
}
