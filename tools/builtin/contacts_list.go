package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type ContactsListTool struct {
	Enabled     bool
	ContactsDir string
}

func NewContactsListTool(enabled bool, contactsDir string) *ContactsListTool {
	return &ContactsListTool{
		Enabled:     enabled,
		ContactsDir: strings.TrimSpace(contactsDir),
	}
}

func (t *ContactsListTool) Name() string { return "contacts_list" }

func (t *ContactsListTool) Description() string {
	return "Lists contacts from contacts store. Use this before proactive sharing to inspect active/inactive contacts and profile metadata."
}

func (t *ContactsListTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"description": "Filter by status: all|active|inactive (default: all).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max records to return (<=0 means all).",
			},
		},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ContactsListTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if !t.Enabled {
		return "", fmt.Errorf("contacts_list tool is disabled")
	}
	dir := pathutil.ExpandHomePath(strings.TrimSpace(t.ContactsDir))
	if dir == "" {
		return "", fmt.Errorf("contacts dir is not configured")
	}

	status := parseContactsStatus(params["status"])
	limit := parseIntDefault(params["limit"], 0)

	svc := contacts.NewService(contacts.NewFileStore(dir))
	records, err := svc.ListContacts(ctx, status)
	if err != nil {
		return "", err
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Status != records[j].Status {
			return records[i].Status < records[j].Status
		}
		return strings.TrimSpace(records[i].ContactID) < strings.TrimSpace(records[j].ContactID)
	})
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}

	out, _ := json.MarshalIndent(map[string]any{
		"count":    len(records),
		"contacts": records,
	}, "", "  ")
	return string(out), nil
}

func parseContactsStatus(raw any) contacts.Status {
	value, _ := raw.(string)
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return ""
	case "active":
		return contacts.StatusActive
	case "inactive":
		return contacts.StatusInactive
	default:
		return ""
	}
}
