package agent

// PendingOutput is returned as Final.Output when the run is paused awaiting an external approval.
// It is intentionally small and safe to serialize (no raw tool params or secrets).
type PendingOutput struct {
	Status            string `json:"status"`
	ApprovalRequestID string `json:"approval_request_id"`
	Message           string `json:"message"`
}
