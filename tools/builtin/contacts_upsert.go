package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type ContactsUpsertTool struct {
	Enabled     bool
	ContactsDir string
}

func NewContactsUpsertTool(enabled bool, contactsDir string) *ContactsUpsertTool {
	return &ContactsUpsertTool{
		Enabled:     enabled,
		ContactsDir: strings.TrimSpace(contactsDir),
	}
}

func (t *ContactsUpsertTool) Name() string { return "contacts_upsert" }

func (t *ContactsUpsertTool) Description() string {
	return "Creates or updates one contact profile with partial-patch semantics (omitted fields are preserved)."
}

func (t *ContactsUpsertTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"contact_id": map[string]any{
				"type":        "string",
				"description": "Stable contact id. Recommended for updates.",
			},
			"kind": map[string]any{
				"type":        "string",
				"description": "Contact kind: agent|human.",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Contact status: active|inactive.",
			},
			"contact_nickname": map[string]any{
				"type":        "string",
				"description": "Display nickname for this contact.",
			},
			"persona_brief": map[string]any{
				"type":        "string",
				"description": "Short personality/interaction summary.",
			},
			"persona_traits": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "number"},
				"description":          "Trait score map: trait->score in [0,1].",
			},
			"pronouns": map[string]any{
				"type":        "string",
				"description": "Optional pronouns for this contact.",
			},
			"timezone": map[string]any{
				"type":        "string",
				"description": "Optional IANA timezone for this contact.",
			},
			"preference_context": map[string]any{
				"type":        "string",
				"description": "Long-form preference/context notes.",
			},
			"subject_id": map[string]any{
				"type":        "string",
				"description": "Subject id. When contact_id is missing, this can be used to derive contact id.",
			},
			"understanding_depth": map[string]any{
				"type":        "number",
				"description": "Understanding depth in [0,100].",
			},
			"topic_weights": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "number"},
				"description":          "Topic affinity map: topic->score in [0,1].",
			},
			"reciprocity_norm": map[string]any{
				"type":        "number",
				"description": "Reciprocity score in [0,1].",
			},
		},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *ContactsUpsertTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || !t.Enabled {
		return "", fmt.Errorf("contacts_upsert tool is disabled")
	}
	contactsDir := pathutil.ExpandHomePath(strings.TrimSpace(t.ContactsDir))
	if contactsDir == "" {
		return "", fmt.Errorf("contacts dir is not configured")
	}

	contactID, hasContactID := optionalStringParam(params, "contact_id")
	subjectID, hasSubjectID := optionalStringParam(params, "subject_id")
	if strings.TrimSpace(contactID) == "" && strings.TrimSpace(subjectID) == "" {
		return "", fmt.Errorf("contact_id or subject_id is required")
	}

	svc := contacts.NewService(contacts.NewFileStore(contactsDir))
	base, found, err := lookupBaseContact(ctx, svc, strings.TrimSpace(contactID), strings.TrimSpace(subjectID))
	if err != nil {
		return "", err
	}
	if !found {
		base = contacts.Contact{
			UnderstandingDepth: 30,
			ReciprocityNorm:    0.5,
		}
		if strings.TrimSpace(subjectID) != "" {
			base.Kind = contacts.KindHuman
		}
	}

	if hasContactID {
		base.ContactID = strings.TrimSpace(contactID)
	}
	if hasSubjectID {
		base.SubjectID = strings.TrimSpace(subjectID)
	}
	if value, ok := optionalStringParam(params, "kind"); ok {
		kind, err := parseUpsertKind(value)
		if err != nil {
			return "", err
		}
		base.Kind = kind
	}
	if value, ok := optionalStringParam(params, "status"); ok {
		status, err := parseUpsertStatus(value)
		if err != nil {
			return "", err
		}
		base.Status = status
	}
	if value, ok := optionalStringParam(params, "contact_nickname"); ok {
		base.ContactNickname = value
	}
	if value, ok := optionalStringParam(params, "persona_brief"); ok {
		base.PersonaBrief = value
	}
	if value, ok := optionalStringParam(params, "pronouns"); ok {
		base.Pronouns = value
	}
	if value, ok := optionalStringParam(params, "timezone"); ok {
		base.Timezone = value
	}
	if value, ok := optionalStringParam(params, "preference_context"); ok {
		base.PreferenceContext = value
	}
	if raw, ok := params["understanding_depth"]; ok {
		base.UnderstandingDepth = parseFloatDefault(raw, base.UnderstandingDepth)
	}
	if raw, ok := params["reciprocity_norm"]; ok {
		base.ReciprocityNorm = parseFloatDefault(raw, base.ReciprocityNorm)
	}
	if raw, ok := params["topic_weights"]; ok {
		values, err := parseNumericMap(raw, "topic_weights")
		if err != nil {
			return "", err
		}
		base.TopicWeights = values
	}
	if raw, ok := params["persona_traits"]; ok {
		values, err := parseNumericMap(raw, "persona_traits")
		if err != nil {
			return "", err
		}
		base.PersonaTraits = values
	}

	updated, err := svc.UpsertContact(ctx, base, time.Now().UTC())
	if err != nil {
		return "", err
	}

	out, _ := json.MarshalIndent(map[string]any{
		"contact": updated,
	}, "", "  ")
	return string(out), nil
}

func lookupBaseContact(ctx context.Context, svc *contacts.Service, contactID string, subjectID string) (contacts.Contact, bool, error) {
	if svc == nil {
		return contacts.Contact{}, false, fmt.Errorf("nil contacts service")
	}
	ids := []string{contactID, subjectID}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		item, ok, err := svc.GetContact(ctx, id)
		if err != nil {
			return contacts.Contact{}, false, err
		}
		if ok {
			return item, true, nil
		}
	}
	return contacts.Contact{}, false, nil
}

func optionalStringParam(params map[string]any, key string) (string, bool) {
	raw, ok := params[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), true
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v)), true
	}
}

func parseUpsertStatus(raw string) (contacts.Status, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "active":
		return contacts.StatusActive, nil
	case "inactive":
		return contacts.StatusInactive, nil
	case "":
		return contacts.StatusActive, nil
	default:
		return "", fmt.Errorf("invalid status %q (want active|inactive)", raw)
	}
}

func parseUpsertKind(raw string) (contacts.Kind, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "agent":
		return contacts.KindAgent, nil
	case "human":
		return contacts.KindHuman, nil
	default:
		return "", fmt.Errorf("invalid kind %q (want agent|human)", raw)
	}
}

func parseNumericMap(raw any, fieldName string) (map[string]float64, error) {
	switch v := raw.(type) {
	case map[string]float64:
		if len(v) == 0 {
			return nil, nil
		}
		out := make(map[string]float64, len(v))
		for key, score := range v {
			nKey := strings.TrimSpace(key)
			if nKey == "" {
				continue
			}
			out[nKey] = score
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	case map[string]any:
		if len(v) == 0 {
			return nil, nil
		}
		out := make(map[string]float64, len(v))
		for key, rawScore := range v {
			nKey := strings.TrimSpace(key)
			if nKey == "" {
				continue
			}
			score, err := toFloat64(rawScore)
			if err != nil {
				return nil, fmt.Errorf("%s[%q]: %w", fieldName, key, err)
			}
			out[nKey] = score
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be an object map", fieldName)
	}
}

func toFloat64(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0, fmt.Errorf("empty number")
		}
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid number %q", text)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported number type %T", raw)
	}
}
