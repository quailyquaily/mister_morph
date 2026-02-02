package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

type Decision string

const (
	DecisionAllow            Decision = "allow"
	DecisionAllowWithRedact  Decision = "allow_with_redaction"
	DecisionRequireApproval  Decision = "require_approval"
	DecisionDeny             Decision = "deny"
)

type ActionType string

const (
	ActionToolCallPre  ActionType = "ToolCallPre"
	ActionToolCallPost ActionType = "ToolCallPost"
	ActionOutputPublish ActionType = "OutputPublish"
	ActionSkillInstall ActionType = "SkillInstall"
)

type Meta struct {
	RunID string
	Step  int
	Time  time.Time
}

type Action struct {
	Type ActionType

	ToolName   string
	ToolParams map[string]any

	Content string

	URL    string
	Method string
}

type Result struct {
	RiskLevel RiskLevel
	Decision  Decision
	Reasons   []string

	RedactedContent string
}

type AuditEvent struct {
	EventID    string    `json:"event_id"`
	RunID      string    `json:"run_id"`
	Timestamp  time.Time `json:"ts"`
	Step       int       `json:"step"`
	ActionType ActionType `json:"action_type"`
	ToolName   string    `json:"tool_name,omitempty"`

	ActionSummaryRedacted string   `json:"action_summary_redacted"`
	ActionHash            string   `json:"action_hash,omitempty"`

	RiskLevel RiskLevel `json:"risk_level"`
	Decision  Decision  `json:"decision"`
	Reasons   []string  `json:"reasons,omitempty"`

	ApprovalRequestID string `json:"approval_request_id,omitempty"`
	ApprovalStatus    string `json:"approval_status,omitempty"`
	Actor             string `json:"actor,omitempty"`
}

func newEventID(meta Meta) string {
	seed := fmt.Sprintf("%s|%d|%s", meta.RunID, meta.Step, meta.Time.UTC().Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(seed))
	return "evt_" + hex.EncodeToString(sum[:8])
}

func canonicalJSON(v any) ([]byte, error) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ordered := make([]any, 0, len(keys)*2)
		for _, k := range keys {
			ordered = append(ordered, k)
			orderedVal, err := canonicalizeValue(x[k])
			if err != nil {
				return nil, err
			}
			ordered = append(ordered, orderedVal)
		}
		return json.Marshal(ordered)
	default:
		cv, err := canonicalizeValue(v)
		if err != nil {
			return nil, err
		}
		return json.Marshal(cv)
	}
}

func canonicalizeValue(v any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]any, 0, len(keys)*2)
		for _, k := range keys {
			out = append(out, k)
			vv, err := canonicalizeValue(x[k])
			if err != nil {
				return nil, err
			}
			out = append(out, vv)
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(x))
		for _, vv := range x {
			cv, err := canonicalizeValue(vv)
			if err != nil {
				return nil, err
			}
			out = append(out, cv)
		}
		return out, nil
	case string, float64, bool, nil, int, int64, json.Number:
		return x, nil
	default:
		// Best-effort for JSON-ish values.
		b, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("cannot canonicalize value of type %T", v)
		}
		var y any
		if err := json.Unmarshal(b, &y); err != nil {
			return nil, fmt.Errorf("cannot canonicalize value of type %T", v)
		}
		return canonicalizeValue(y)
	}
}

func ActionHash(a Action) (string, error) {
	// Only include stable fields. Content is included for OutputPublish.
	payload := map[string]any{
		"type": a.Type,
	}
	if strings.TrimSpace(a.ToolName) != "" {
		payload["tool_name"] = a.ToolName
	}
	if a.ToolParams != nil {
		payload["tool_params"] = a.ToolParams
	}
	if strings.TrimSpace(a.Content) != "" {
		payload["content"] = a.Content
	}
	if strings.TrimSpace(a.URL) != "" {
		payload["url"] = a.URL
	}
	if strings.TrimSpace(a.Method) != "" {
		payload["method"] = a.Method
	}

	b, err := canonicalJSON(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

