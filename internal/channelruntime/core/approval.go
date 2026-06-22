package core

import (
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
)

func PendingApprovalID(final *agent.Final) (string, bool) {
	if final == nil || final.Output == nil {
		return "", false
	}
	switch v := final.Output.(type) {
	case agent.PendingOutput:
		if strings.EqualFold(strings.TrimSpace(v.Status), "pending") && strings.TrimSpace(v.ApprovalRequestID) != "" {
			return strings.TrimSpace(v.ApprovalRequestID), true
		}
	case *agent.PendingOutput:
		if v != nil && strings.EqualFold(strings.TrimSpace(v.Status), "pending") && strings.TrimSpace(v.ApprovalRequestID) != "" {
			return strings.TrimSpace(v.ApprovalRequestID), true
		}
	case map[string]any:
		st, _ := v["status"].(string)
		id, _ := v["approval_request_id"].(string)
		if strings.EqualFold(strings.TrimSpace(st), "pending") && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id), true
		}
	}
	return "", false
}
