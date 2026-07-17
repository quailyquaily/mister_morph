package runtimecommands

import (
	"strings"
	"testing"
)

func TestSuggestionsMatchRuntimeCommands(t *testing.T) {
	items := Suggestions()
	if len(items) == 0 {
		t.Fatal("Suggestions() returned no items")
	}

	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.Value)
		if !strings.HasPrefix(item.Value, "/") {
			t.Fatalf("item.Value = %q, want slash command", item.Value)
		}
		if strings.TrimSpace(item.Title) == "" {
			t.Fatalf("item %q has empty Title", item.Value)
		}
		if !strings.HasSuffix(item.InsertText, " ") {
			t.Fatalf("item %q InsertText = %q, want trailing space", item.Value, item.InsertText)
		}
	}

	wantPrefix := []string{
		"/help",
		"/stop",
		"/models",
		"/skills",
		"/ctx",
		"/workspace",
		"/think",
	}
	if len(values) < len(wantPrefix) {
		t.Fatalf("values = %v, want at least %d items", values, len(wantPrefix))
	}
	for i, want := range wantPrefix {
		if values[i] != want {
			t.Fatalf("values[%d] = %q, want %q; all=%v", i, values[i], want, values)
		}
	}
	if !containsString(values, "/models list") {
		t.Fatalf("values = %v, want /models list", values)
	}
	if !containsString(values, "/ctx compact") {
		t.Fatalf("values = %v, want /ctx compact", values)
	}
	if !containsString(values, "/workspace attach") {
		t.Fatalf("values = %v, want /workspace attach", values)
	}
	for _, unexpected := range []string{"/plan", "/review", "/fix"} {
		if containsString(values, unexpected) {
			t.Fatalf("values contains placeholder command %q: %v", unexpected, values)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
