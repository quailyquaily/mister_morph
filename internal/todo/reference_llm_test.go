package todo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLLMReferenceResolverResolveAddContentOK(t *testing.T) {
	client := &stubTodoLLMClient{
		replies: []string{
			`{"status":"ok","rewritten_content":"提醒 [John](tg:1001) 明天确认","warnings":["  x  ","x",""]}`,
		},
	}
	resolver := NewLLMReferenceResolver(client, "gpt-5.2")
	rewritten, warnings, err := resolver.ResolveAddContent(context.Background(), "提醒 John 明天确认", []string{"John"}, ContactSnapshot{
		Contacts: []ContactSnapshotItem{
			{Name: "John", ReachableIDs: []string{"tg:1001"}, PreferredID: "tg:1001"},
		},
		ReachableIDs: []string{"tg:1001"},
	}, AddResolveContext{
		Channel:         "telegram",
		ChatType:        "private",
		ChatID:          1001,
		SpeakerUserID:   1001,
		SpeakerUsername: "john",
		UserInputRaw:    "今晚九点提醒我和 John 看球赛",
	})
	if err != nil {
		t.Fatalf("ResolveAddContent() error = %v", err)
	}
	if rewritten != "提醒 [John](tg:1001) 明天确认" {
		t.Fatalf("rewritten mismatch: %q", rewritten)
	}
	if len(warnings) != 1 || warnings[0] != "x" {
		t.Fatalf("warnings mismatch: %#v", warnings)
	}
	if len(client.calls) != 1 {
		t.Fatalf("expected one llm call, got %d", len(client.calls))
	}
	if !client.calls[0].ForceJSON {
		t.Fatalf("expected ForceJSON=true")
	}
	if !strings.Contains(client.calls[0].Messages[0].Content, "YYYY-MM-DD hh:mm") {
		t.Fatalf("system prompt missing time rewrite rule: %q", client.calls[0].Messages[0].Content)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(client.calls[0].Messages[1].Content), &payload); err != nil {
		t.Fatalf("payload json parse error: %v", err)
	}
	input, _ := payload["input"].(map[string]any)
	if input == nil {
		t.Fatalf("missing input payload")
	}
	toolPeople, _ := input["people"].([]any)
	if len(toolPeople) != 1 || strings.TrimSpace(toolPeople[0].(string)) != "John" {
		t.Fatalf("unexpected input.people payload: %#v", input["people"])
	}
	if strings.TrimSpace(payload["input_raw"].(string)) == "" {
		t.Fatalf("expected non-empty input_raw")
	}
	runtime, _ := payload["runtime"].(map[string]any)
	if runtime == nil {
		t.Fatalf("missing runtime payload")
	}
	for _, key := range []string{"now_utc", "now_local", "now_local_weekday"} {
		v, _ := runtime[key].(string)
		if strings.TrimSpace(v) == "" {
			t.Fatalf("missing runtime clock field %q in runtime payload: %#v", key, runtime)
		}
	}
}

func TestLLMReferenceResolverResolveAddContentMissingReference(t *testing.T) {
	client := &stubTodoLLMClient{
		replies: []string{
			`{"status":"missing_reference_id","missing":[{"mention":"John","suggestion":"[John](tg:1001)"}]}`,
		},
	}
	resolver := NewLLMReferenceResolver(client, "gpt-5.2")
	_, _, err := resolver.ResolveAddContent(context.Background(), "提醒 John 明天确认", []string{"John"}, ContactSnapshot{}, AddResolveContext{})
	if err == nil {
		t.Fatalf("expected missing_reference_id error")
	}
	var missingErr *MissingReferenceIDError
	if !strings.Contains(strings.ToLower(err.Error()), "missing_reference_id") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "John") {
		t.Fatalf("missing error detail: %v", err)
	}
	if !asMissingReferenceError(err, &missingErr) || missingErr == nil || len(missingErr.Items) != 1 {
		t.Fatalf("expected MissingReferenceIDError, got %T", err)
	}
}

func TestLLMReferenceResolverResolveAddContentParsesJSONCodeFence(t *testing.T) {
	client := &stubTodoLLMClient{
		replies: []string{"```json\n{\"status\":\"ok\",\"rewritten_content\":\"x\"}\n```"},
	}
	resolver := NewLLMReferenceResolver(client, "gpt-5.2")
	rewritten, warnings, err := resolver.ResolveAddContent(context.Background(), "提醒 John 明天确认", []string{"John"}, ContactSnapshot{}, AddResolveContext{})
	if err != nil {
		t.Fatalf("expected code-fence json to be parsed, got %v", err)
	}
	if rewritten != "x" {
		t.Fatalf("rewritten mismatch: %q", rewritten)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings mismatch: %#v", warnings)
	}
}

func TestLLMReferenceResolverResolveAddContentRejectsInvalidJSON(t *testing.T) {
	client := &stubTodoLLMClient{
		replies: []string{"not a json payload"},
	}
	resolver := NewLLMReferenceResolver(client, "gpt-5.2")
	_, _, err := resolver.ResolveAddContent(context.Background(), "提醒 John 明天确认", []string{"John"}, ContactSnapshot{}, AddResolveContext{})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid reference_resolve response") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func asMissingReferenceError(err error, target **MissingReferenceIDError) bool {
	if err == nil || target == nil {
		return false
	}
	v, ok := err.(*MissingReferenceIDError)
	if !ok {
		return false
	}
	*target = v
	return true
}
