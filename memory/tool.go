package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mister_morph/tools"
)

type ToolSet struct {
	Store     Store
	SubjectID string
	Context   RequestContext
	Source    string
}

func (s ToolSet) All() []tools.Tool {
	return []tools.Tool{
		&memoryGetTool{s: s},
		&memoryPutTool{s: s},
		&memoryListTool{s: s},
		&memoryDeleteTool{s: s},
		&memoryDeleteNamespaceTool{s: s},
	}
}

type memoryGetTool struct{ s ToolSet }

func (t *memoryGetTool) Name() string { return "memory_get" }
func (t *memoryGetTool) Description() string {
	return "Get one long-term memory item for the current user (subject). Respects public/private visibility rules."
}
func (t *memoryGetTool) ParameterSchema() string {
	return mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"namespace": map[string]any{"type": "string", "description": "Namespace: profile|preference|fact|project|task_state."},
			"key":       map[string]any{"type": "string", "description": "Item key."},
		},
		"required": []string{"namespace", "key"},
	})
}
func (t *memoryGetTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	ns := strings.TrimSpace(asString(params["namespace"]))
	key := strings.TrimSpace(asString(params["key"]))
	if !IsAllowedNamespace(ns) {
		return "", fmt.Errorf("invalid namespace: %q", ns)
	}
	if key == "" {
		return "", fmt.Errorf("missing required param: key")
	}

	it, ok, err := t.s.Store.Get(ctx, t.s.SubjectID, ns, key, ReadOptions{Context: t.s.Context})
	if err != nil {
		return "", err
	}
	out := map[string]any{"ok": ok}
	if ok {
		out["item"] = itemToJSON(it, true)
	}
	return mustJSON(out), nil
}

type memoryPutTool struct{ s ToolSet }

func (t *memoryPutTool) Name() string { return "memory_put" }
func (t *memoryPutTool) Description() string {
	return "Create or update a long-term memory item for the current user (subject). Default visibility is private_only."
}
func (t *memoryPutTool) ParameterSchema() string {
	return mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"namespace": map[string]any{"type": "string", "description": "Namespace: profile|preference|fact|project|task_state."},
			"key":       map[string]any{"type": "string", "description": "Item key."},
			"value":     map[string]any{"type": "string", "description": "Item value (JSON string or plain text)."},
			"visibility": map[string]any{
				"type":        "string",
				"description": "Optional visibility: private_only|public_ok (default: private_only).",
				"enum":        []string{"private_only", "public_ok"},
			},
		},
		"required": []string{"namespace", "key", "value"},
	})
}
func (t *memoryPutTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	ns := strings.TrimSpace(asString(params["namespace"]))
	key := strings.TrimSpace(asString(params["key"]))
	val := strings.TrimSpace(asString(params["value"]))
	if !IsAllowedNamespace(ns) {
		return "", fmt.Errorf("invalid namespace: %q", ns)
	}
	if key == "" {
		return "", fmt.Errorf("missing required param: key")
	}
	if val == "" {
		return "", fmt.Errorf("missing required param: value")
	}

	vis := PrivateOnly
	if v, ok := params["visibility"]; ok {
		switch strings.ToLower(strings.TrimSpace(asString(v))) {
		case "", "private_only":
			vis = PrivateOnly
		case "public_ok":
			vis = PublicOK
		default:
			return "", fmt.Errorf("invalid visibility: %q", asString(v))
		}
	}

	var src *string
	if strings.TrimSpace(t.s.Source) != "" {
		s := t.s.Source
		src = &s
	}
	it, err := t.s.Store.Put(ctx, t.s.SubjectID, ns, key, val, PutOptions{
		Visibility: &vis,
		Source:     src,
	})
	if err != nil {
		return "", err
	}
	includeValue := t.s.Context == ContextPrivate || vis == PublicOK
	return mustJSON(map[string]any{"ok": true, "item": itemToJSON(it, includeValue)}), nil
}

type memoryListTool struct{ s ToolSet }

func (t *memoryListTool) Name() string { return "memory_list" }
func (t *memoryListTool) Description() string {
	return "List long-term memory items for the current user (subject) within a namespace. Respects public/private visibility rules."
}
func (t *memoryListTool) ParameterSchema() string {
	return mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"namespace": map[string]any{"type": "string", "description": "Namespace: profile|preference|fact|project|task_state."},
			"prefix":    map[string]any{"type": "string", "description": "Optional key prefix."},
			"limit":     map[string]any{"type": "integer", "description": "Optional max items (default: 50)."},
		},
		"required": []string{"namespace"},
	})
}
func (t *memoryListTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	ns := strings.TrimSpace(asString(params["namespace"]))
	if !IsAllowedNamespace(ns) {
		return "", fmt.Errorf("invalid namespace: %q", ns)
	}
	prefix := strings.TrimSpace(asString(params["prefix"]))
	limit := 50
	if v, ok := params["limit"]; ok {
		if n, ok := asInt(v); ok && n > 0 {
			limit = n
		}
	}

	items, err := t.s.Store.List(ctx, t.s.SubjectID, ns, ReadOptions{Context: t.s.Context, Prefix: prefix, Limit: limit})
	if err != nil {
		return "", err
	}
	outItems := make([]any, 0, len(items))
	for _, it := range items {
		outItems = append(outItems, itemToJSON(it, true))
	}
	return mustJSON(map[string]any{"ok": true, "items": outItems}), nil
}

type memoryDeleteTool struct{ s ToolSet }

func (t *memoryDeleteTool) Name() string { return "memory_delete" }
func (t *memoryDeleteTool) Description() string {
	return "Delete one long-term memory item for the current user (subject)."
}
func (t *memoryDeleteTool) ParameterSchema() string {
	return mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"namespace": map[string]any{"type": "string", "description": "Namespace: profile|preference|fact|project|task_state."},
			"key":       map[string]any{"type": "string", "description": "Item key."},
		},
		"required": []string{"namespace", "key"},
	})
}
func (t *memoryDeleteTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	ns := strings.TrimSpace(asString(params["namespace"]))
	key := strings.TrimSpace(asString(params["key"]))
	if !IsAllowedNamespace(ns) {
		return "", fmt.Errorf("invalid namespace: %q", ns)
	}
	if key == "" {
		return "", fmt.Errorf("missing required param: key")
	}
	if err := t.s.Store.Delete(ctx, t.s.SubjectID, ns, key); err != nil {
		return "", err
	}
	return mustJSON(map[string]any{"ok": true}), nil
}

type memoryDeleteNamespaceTool struct{ s ToolSet }

func (t *memoryDeleteNamespaceTool) Name() string { return "memory_delete_namespace" }
func (t *memoryDeleteNamespaceTool) Description() string {
	return "Delete all long-term memory items for the current user (subject) in a namespace."
}
func (t *memoryDeleteNamespaceTool) ParameterSchema() string {
	return mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"namespace": map[string]any{"type": "string", "description": "Namespace: profile|preference|fact|project|task_state."},
		},
		"required": []string{"namespace"},
	})
}
func (t *memoryDeleteNamespaceTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	ns := strings.TrimSpace(asString(params["namespace"]))
	if !IsAllowedNamespace(ns) {
		return "", fmt.Errorf("invalid namespace: %q", ns)
	}
	if err := t.s.Store.DeleteNamespace(ctx, t.s.SubjectID, ns); err != nil {
		return "", err
	}
	return mustJSON(map[string]any{"ok": true}), nil
}

func itemToJSON(it Item, includeValue bool) map[string]any {
	out := map[string]any{
		"subject_id": it.SubjectID,
		"namespace":  it.Namespace,
		"key":        it.Key,
		"visibility": visibilityString(it.Visibility),
		"created_at": it.CreatedAt,
		"updated_at": it.UpdatedAt,
		"confidence": it.Confidence,
		"source":     it.Source,
	}
	if includeValue {
		out["value"] = it.Value
	}
	return out
}

func visibilityString(v Visibility) string {
	switch v {
	case PublicOK:
		return "public_ok"
	case PrivateOnly:
		return "private_only"
	default:
		return "private_only"
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return strings.TrimSpace(string(b))
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		return int(n), err == nil
	case string:
		x = strings.TrimSpace(x)
		if x == "" {
			return 0, false
		}
		var n json.Number = json.Number(x)
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func mustJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
