package agent

import "strings"

// PendingOutput is returned as Final.Output when the run is paused awaiting an external approval.
// It is intentionally small and safe to serialize (no raw tool params or secrets).
type PendingOutput struct {
	Status            string `json:"status"`
	ApprovalRequestID string `json:"approval_request_id"`
	Message           string `json:"message"`
}

// PendingApprovalTool is the exact tool invocation bound to an approval.
type PendingApprovalTool struct {
	Name   string
	Params map[string]any
}

// PendingApprovalToolFromResumeState exposes only the pending tool invocation,
// keeping the rest of the private resume format inside the agent package.
func PendingApprovalToolFromResumeState(data []byte) (PendingApprovalTool, bool) {
	state, err := unmarshalResumeState(data)
	if err != nil {
		return PendingApprovalTool{}, false
	}
	name := strings.TrimSpace(state.PendingTool.ToolCall.Name)
	if name == "" {
		return PendingApprovalTool{}, false
	}
	return PendingApprovalTool{
		Name:   name,
		Params: state.PendingTool.ToolCall.Params,
	}, true
}
