package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestContactsUpsertTool_CreateBySubjectID(t *testing.T) {
	tool := NewContactsUpsertTool(true, t.TempDir())
	out, err := tool.Execute(context.Background(), map[string]any{
		"subject_id":         "tg:@alice",
		"status":             "active",
		"contact_nickname":   "Alice",
		"pronouns":           "she/her",
		"timezone":           "America/New_York",
		"preference_context": "prefers concise updates",
		"topic_weights": map[string]any{
			"go":  0.8,
			"ops": 0.5,
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := parseUpsertContact(t, out)
	if got.ContactID != "tg:@alice" {
		t.Fatalf("contact_id mismatch: got %q", got.ContactID)
	}
	if got.SubjectID != "tg:@alice" {
		t.Fatalf("subject_id mismatch: got %q", got.SubjectID)
	}
	if got.Kind != "human" {
		t.Fatalf("kind mismatch: got %q want human", got.Kind)
	}
	if got.ContactNickname != "Alice" {
		t.Fatalf("contact_nickname mismatch: got %q", got.ContactNickname)
	}
	if got.Pronouns != "she/her" {
		t.Fatalf("pronouns mismatch: got %q", got.Pronouns)
	}
	if got.Timezone != "America/New_York" {
		t.Fatalf("timezone mismatch: got %q", got.Timezone)
	}
	if got.PreferenceContext != "prefers concise updates" {
		t.Fatalf("preference_context mismatch: got %q", got.PreferenceContext)
	}
	if len(got.TopicWeights) != 2 {
		t.Fatalf("topic_weights mismatch: got %#v", got.TopicWeights)
	}
}

func TestContactsUpsertTool_PartialPatchPreservesFields(t *testing.T) {
	tool := NewContactsUpsertTool(true, t.TempDir())
	_, err := tool.Execute(context.Background(), map[string]any{
		"subject_id":       "tg:@alice",
		"persona_brief":    "likes deep technical discussion",
		"contact_nickname": "Alice",
	})
	if err != nil {
		t.Fatalf("seed Execute() error = %v", err)
	}

	out, err := tool.Execute(context.Background(), map[string]any{
		"contact_id":       "tg:@alice",
		"contact_nickname": "Alice L",
	})
	if err != nil {
		t.Fatalf("patch Execute() error = %v", err)
	}
	got := parseUpsertContact(t, out)
	if got.ContactNickname != "Alice L" {
		t.Fatalf("contact_nickname mismatch: got %q", got.ContactNickname)
	}
	if got.PersonaBrief != "likes deep technical discussion" {
		t.Fatalf("persona_brief should be preserved, got %q", got.PersonaBrief)
	}
}

func TestContactsUpsertTool_MissingIdentifiers(t *testing.T) {
	tool := NewContactsUpsertTool(true, t.TempDir())
	_, err := tool.Execute(context.Background(), map[string]any{
		"status": "active",
	})
	if err == nil || !strings.Contains(err.Error(), "contact_id or subject_id is required") {
		t.Fatalf("expected missing id error, got %v", err)
	}
}

func TestContactsUpsertTool_InvalidStatus(t *testing.T) {
	tool := NewContactsUpsertTool(true, t.TempDir())
	_, err := tool.Execute(context.Background(), map[string]any{
		"subject_id": "tg:@alice",
		"status":     "invalid",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}

func TestContactsUpsertTool_InvalidKind(t *testing.T) {
	tool := NewContactsUpsertTool(true, t.TempDir())
	_, err := tool.Execute(context.Background(), map[string]any{
		"subject_id": "tg:@alice",
		"kind":       "robot",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid kind") {
		t.Fatalf("expected invalid kind error, got %v", err)
	}
}

type upsertToolContact struct {
	ContactID         string             `json:"contact_id"`
	Kind              string             `json:"kind"`
	Status            string             `json:"status"`
	ContactNickname   string             `json:"contact_nickname"`
	PersonaBrief      string             `json:"persona_brief"`
	Pronouns          string             `json:"pronouns"`
	Timezone          string             `json:"timezone"`
	PreferenceContext string             `json:"preference_context"`
	SubjectID         string             `json:"subject_id"`
	TopicWeights      map[string]float64 `json:"topic_weights"`
}

func parseUpsertContact(t *testing.T, raw string) upsertToolContact {
	t.Helper()
	var out struct {
		Contact upsertToolContact `json:"contact"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return out.Contact
}
