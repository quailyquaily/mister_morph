package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type AgentSendTool struct {
	opts ContactsSendToolOptions
}

func NewAgentSendTool(opts ContactsSendToolOptions) *AgentSendTool {
	return &AgentSendTool{opts: opts}
}

func (t *AgentSendTool) Name() string { return "agent_send" }

func (t *AgentSendTool) Description() string {
	return `Sends a message to one or more active Agent contacts.
		IF sending to multiple Agents THEN pass comma-separated contact_id values.
		Message routes automatically across Slack, Telegram, LINE, and Lark based on chat_id/contact reachability.
		Only target Agents explicitly referenced in the current task.`
}

func (t *AgentSendTool) ParameterSchema() string {
	return contactSendParameterSchema()
}

func (t *AgentSendTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil {
		return "", fmt.Errorf("agent_send tool is disabled")
	}
	return executeContactSendTool(ctx, params, t.opts, contactSendExecutionPolicy{
		toolName:         "agent_send",
		activeAgentsOnly: true,
	})
}

func AgentSendAvailable(ctx context.Context, contactsDir string) (bool, error) {
	contactsDir = pathutil.ExpandHomePath(strings.TrimSpace(contactsDir))
	if contactsDir == "" {
		return false, nil
	}
	ids, err := loadActiveAgentContactIDs(ctx, contacts.NewFileStore(contactsDir))
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

func loadActiveAgentContactIDs(ctx context.Context, store contacts.ContactStore) (map[string]struct{}, error) {
	if store == nil {
		return nil, fmt.Errorf("contacts store is required")
	}
	active, err := store.ListContacts(ctx, contacts.StatusActive)
	if err != nil {
		return nil, fmt.Errorf("load active Agents: %w", err)
	}
	ids := make(map[string]struct{})
	for _, contact := range active {
		if contact.Kind != contacts.KindAgent {
			continue
		}
		if id := normalizeAgentSendContactID(contact.ContactID); id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids, nil
}

func validateActiveAgentRecipient(activeAgentIDs map[string]struct{}, contactID string, contact contacts.Contact) error {
	_, active := activeAgentIDs[normalizeAgentSendContactID(contact.ContactID)]
	if contact.Kind != contacts.KindAgent || !active {
		return fmt.Errorf("agent_send target %q is not an active Agent", strings.TrimSpace(contactID))
	}
	return nil
}

func normalizeAgentSendContactID(contactID string) string {
	return strings.ToLower(strings.TrimSpace(contactID))
}
