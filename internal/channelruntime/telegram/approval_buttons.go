package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/quailyquaily/mistermorph/guard"
)

const telegramApprovalCallbackPrefix = "ap:"

func telegramApprovalCallbackData(approvalRequestID string, approved bool) string {
	approvalRequestID = strings.TrimSpace(approvalRequestID)
	if approvalRequestID == "" {
		return ""
	}
	decision := "d"
	if approved {
		decision = "a"
	}
	return telegramApprovalCallbackPrefix + decision + ":" + approvalRequestID
}

func parseTelegramApprovalCallbackData(raw string) (approvalRequestID string, approved bool, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, telegramApprovalCallbackPrefix) {
		return "", false, false
	}
	parts := strings.SplitN(strings.TrimPrefix(raw, telegramApprovalCallbackPrefix), ":", 2)
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

func telegramApprovalReplyMarkup(approvalRequestID string) *telegramInlineKeyboardMarkup {
	approveData := telegramApprovalCallbackData(approvalRequestID, true)
	denyData := telegramApprovalCallbackData(approvalRequestID, false)
	if approveData == "" || denyData == "" {
		return nil
	}
	return &telegramInlineKeyboardMarkup{
		InlineKeyboard: [][]telegramInlineKeyboardButton{
			{
				{Text: "Approve", CallbackData: approveData},
				{Text: "Deny", CallbackData: denyData},
			},
		},
	}
}

func telegramApprovalResultText(approved bool) string {
	if approved {
		return "Approved. Resuming task."
	}
	return "Approval denied. Task canceled."
}

func telegramApprovalCallbackMessageTarget(query *telegramCallbackQuery) (chatID int64, messageThreadID int64, ok bool) {
	if query == nil || query.Message == nil || query.Message.Chat == nil || query.Message.Chat.ID == 0 {
		return 0, 0, false
	}
	return query.Message.Chat.ID, query.Message.MessageThreadID, true
}

func telegramApprovalActor(from *telegramUser) string {
	if from == nil {
		return "telegram:unknown"
	}
	if username := strings.TrimSpace(from.Username); username != "" {
		return "telegram:@" + username
	}
	if from.ID != 0 {
		return "telegram:" + strconv.FormatInt(from.ID, 10)
	}
	return "telegram:unknown"
}

func telegramApprovalRequestText(job telegramJob, rec guard.ApprovalRecord) string {
	parts := []string{"Approval required"}
	if toolName := strings.TrimSpace(rec.ToolName); toolName != "" {
		parts = append(parts, "Tool: "+toolName)
	}
	if summary := strings.TrimSpace(rec.ActionSummaryRedacted); summary != "" {
		parts = append(parts, "Action: "+truncateTelegramApprovalLine(summary, 900))
	}
	if task := strings.TrimSpace(job.Text); task != "" {
		parts = append(parts, "Task: "+truncateTelegramApprovalLine(task, 700))
	}
	if id := strings.TrimSpace(rec.ID); id != "" {
		parts = append(parts, fmt.Sprintf("approval_request_id: %s", id))
	}
	return strings.Join(parts, "\n")
}

func truncateTelegramApprovalLine(raw string, max int) string {
	raw = strings.TrimSpace(raw)
	if max <= 0 || len(raw) <= max {
		return raw
	}
	return strings.TrimSpace(raw[:max]) + "..."
}
