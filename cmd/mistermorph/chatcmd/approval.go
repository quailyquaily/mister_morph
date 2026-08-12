package chatcmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
)

type chatApprovalDecision uint8

const (
	chatApprovalUndecided chatApprovalDecision = iota
	chatApprovalApprove
	chatApprovalDeny
	chatApprovalExpired
)

func parseChatApprovalDecision(input string) chatApprovalDecision {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes", "approve", "/approve":
		return chatApprovalApprove
	case "n", "no", "deny", "/deny", "/stop":
		return chatApprovalDeny
	default:
		return chatApprovalUndecided
	}
}

func resolveChatApprovalInput(
	ctx context.Context,
	approvalGuard *guard.Guard,
	approvalID string,
	input string,
	actor string,
	now time.Time,
) (chatApprovalDecision, runtimecore.ApprovalCommitState, error) {
	decision := parseChatApprovalDecision(input)
	if decision == chatApprovalUndecided {
		return decision, runtimecore.ApprovalCommitUnknown, nil
	}
	if approvalGuard == nil {
		return decision, runtimecore.ApprovalCommitUnknown, errors.New("approvals are unavailable")
	}
	record, found, err := approvalGuard.GetApproval(ctx, approvalID)
	if err != nil {
		return decision, runtimecore.ApprovalCommitUnknown, err
	}
	if !found {
		return decision, runtimecore.ApprovalCommitUnknown, guard.ErrApprovalNotFound
	}

	status := guard.ApprovalDenied
	comment := ""
	if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
		decision = chatApprovalExpired
		status = guard.ApprovalExpired
		actor = "chat:expiry"
		comment = "approval expired"
	} else if decision == chatApprovalApprove {
		status = guard.ApprovalApproved
	}
	state, _, err := runtimecore.ResolveApprovalCommit(ctx, approvalGuard, approvalID, status, actor, comment)
	return decision, state, err
}

func formatChatApprovalRequest(record guard.ApprovalRecord) string {
	var lines []string
	lines = append(lines, "Approval required")
	if toolName := strings.TrimSpace(record.ToolName); toolName != "" {
		lines = append(lines, "Tool: "+toolName)
	}

	if params := runtimecore.ApprovalToolParams(record); len(params) > 0 {
		if payload, err := json.MarshalIndent(params, "", "  "); err == nil {
			lines = append(lines, "Parameters:\n"+string(payload))
		}
	} else if summary := strings.TrimSpace(record.ActionSummaryRedacted); summary != "" {
		lines = append(lines, "Action: "+summary)
	}

	reasons := make([]string, 0, len(record.Reasons))
	for _, reason := range record.Reasons {
		if reason = strings.TrimSpace(reason); reason != "" {
			reasons = append(reasons, "- "+reason)
		}
	}
	if len(reasons) > 0 {
		lines = append(lines, "Reasons:\n"+strings.Join(reasons, "\n"))
	}

	lines = append(lines, "Enter /approve or y to continue; /deny or n to cancel.")
	return strings.Join(lines, "\n")
}
