package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type ContactsFeedbackUpdateTool struct {
	Enabled     bool
	ContactsDir string
}

func NewContactsFeedbackUpdateTool(enabled bool, contactsDir string) *ContactsFeedbackUpdateTool {
	return &ContactsFeedbackUpdateTool{
		Enabled:     enabled,
		ContactsDir: strings.TrimSpace(contactsDir),
	}
}

func (t *ContactsFeedbackUpdateTool) Name() string { return "contacts_feedback_update" }

func (t *ContactsFeedbackUpdateTool) Description() string {
	return "Applies one interaction feedback signal to a contact and session (interest, reciprocity, depth, topic preference)."
}

func (t *ContactsFeedbackUpdateTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"contact_id": map[string]any{
				"type":        "string",
				"description": "Target contact_id.",
			},
			"signal": map[string]any{
				"type":        "string",
				"description": "Feedback signal: positive|neutral|negative.",
			},
			"topic": map[string]any{
				"type":        "string",
				"description": "Optional topic for preference update.",
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "Optional session id. Default contact_id.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Optional reason for audit.",
			},
			"end_session": map[string]any{
				"type":        "boolean",
				"description": "Mark session as ended.",
			},
		},
		"required": []string{"contact_id", "signal"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ContactsFeedbackUpdateTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || !t.Enabled {
		return "", fmt.Errorf("contacts_feedback_update tool is disabled")
	}
	contactsDir := pathutil.ExpandHomePath(strings.TrimSpace(t.ContactsDir))
	if contactsDir == "" {
		return "", fmt.Errorf("contacts dir is not configured")
	}
	contactID, _ := params["contact_id"].(string)
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return "", fmt.Errorf("missing required param: contact_id")
	}
	signalRaw, _ := params["signal"].(string)
	signalRaw = strings.ToLower(strings.TrimSpace(signalRaw))
	if signalRaw == "" {
		return "", fmt.Errorf("missing required param: signal")
	}

	sessionID, _ := params["session_id"].(string)
	topic, _ := params["topic"].(string)
	reason, _ := params["reason"].(string)
	endSession := parseBoolDefault(params["end_session"], false)

	svc := contacts.NewService(contacts.NewFileStore(contactsDir))
	contact, session, err := svc.UpdateFeedback(ctx, time.Now().UTC(), contacts.FeedbackUpdateInput{
		ContactID:  contactID,
		SessionID:  strings.TrimSpace(sessionID),
		Signal:     contacts.FeedbackSignal(signalRaw),
		Topic:      strings.TrimSpace(topic),
		Reason:     strings.TrimSpace(reason),
		EndSession: endSession,
	})
	if err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(map[string]any{
		"contact": contact,
		"session": session,
	}, "", "  ")
	return string(out), nil
}
