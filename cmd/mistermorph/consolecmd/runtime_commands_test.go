package consolecmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleCommandsRouteReturnsRuntimeCommandSuggestions(t *testing.T) {
	srv := &server{cfg: serveConfig{basePath: "/console", passwordOptional: true}}
	req := httptest.NewRequest(http.MethodGet, "/console/api/commands", nil)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Items []struct {
			Value      string `json:"value"`
			InsertText string `json:"insert_text"`
		} `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	values := make([]string, 0, len(payload.Items))
	for _, item := range payload.Items {
		values = append(values, item.Value)
		if !strings.HasSuffix(item.InsertText, " ") {
			t.Fatalf("item %q insert_text = %q, want trailing space", item.Value, item.InsertText)
		}
	}
	if !containsConsoleCommandValue(values, "/help") {
		t.Fatalf("values = %v, want /help", values)
	}
	if !containsConsoleCommandValue(values, "/workspace attach") {
		t.Fatalf("values = %v, want /workspace attach", values)
	}
	if containsConsoleCommandValue(values, "/review") {
		t.Fatalf("values contains placeholder /review: %v", values)
	}
}

func containsConsoleCommandValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
