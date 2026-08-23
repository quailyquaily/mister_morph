package chatcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

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

type chatApprovalViewData struct {
	tool    string
	action  string
	params  []chatApprovalParam
	reasons []string
}

type chatApprovalParam struct {
	name  string
	value string
}

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

func chatApprovalData(record guard.ApprovalRecord) chatApprovalViewData {
	data := chatApprovalViewData{tool: escapeTerminalControls(strings.TrimSpace(record.ToolName))}
	params := runtimecore.ApprovalToolParams(record)
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "cmd" {
			return keys[j] != "cmd"
		}
		if keys[j] == "cmd" {
			return false
		}
		return keys[i] < keys[j]
	})
	data.params = make([]chatApprovalParam, 0, len(keys))
	for _, key := range keys {
		value, ok := params[key].(string)
		if !ok {
			payload, err := json.MarshalIndent(params[key], "", "  ")
			if err != nil {
				value = fmt.Sprint(params[key])
			} else {
				value = string(payload)
			}
		}
		data.params = append(data.params, chatApprovalParam{
			name:  escapeTerminalControls(key),
			value: escapeTerminalControls(value),
		})
	}
	if command, ok := params["cmd"].(string); ok && strings.TrimSpace(command) != "" {
		prefix := "$ "
		if strings.EqualFold(data.tool, "powershell") {
			prefix = "PS> "
		}
		data.action = prefix + escapeTerminalControls(command)
	} else if summary := strings.TrimSpace(record.ActionSummaryRedacted); summary != "" {
		data.action = escapeTerminalControls(summary)
	} else if len(params) > 0 {
		if payload, err := json.Marshal(params); err == nil {
			data.action = escapeTerminalControls(string(payload))
		}
	}

	data.reasons = make([]string, 0, len(record.Reasons))
	for _, reason := range record.Reasons {
		if reason = strings.TrimSpace(reason); reason != "" {
			data.reasons = append(data.reasons, escapeTerminalControls(reason))
		}
	}
	return data
}

func formatChatApprovalOutcome(status string, record guard.ApprovalRecord) string {
	data := chatApprovalData(record)
	parts := []string{escapeTerminalControls(strings.TrimSpace(status))}
	if data.tool != "" {
		parts = append(parts, data.tool)
	}
	if data.action != "" {
		parts = append(parts, strings.TrimPrefix(strings.TrimPrefix(data.action, "$ "), "PS> "))
	}
	return strings.Join(parts, " · ")
}

func escapeTerminalControls(text string) string {
	var escaped strings.Builder
	for _, char := range text {
		if char == '\n' || !unicode.IsControl(char) {
			escaped.WriteRune(char)
			continue
		}
		switch char {
		case '\t':
			escaped.WriteString(`\t`)
		case '\r':
			escaped.WriteString(`\r`)
		default:
			if char <= 0xff {
				_, _ = fmt.Fprintf(&escaped, `\x%02x`, char)
			} else {
				_, _ = fmt.Fprintf(&escaped, `\u%04x`, char)
			}
		}
	}
	return escaped.String()
}
