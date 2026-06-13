package daemonruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandsRouteReturnsRuntimeCommandSuggestions(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
	})

	req := httptest.NewRequest(http.MethodGet, "/commands", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Items []struct {
			Value       string `json:"value"`
			Title       string `json:"title"`
			Description string `json:"description"`
			InsertText  string `json:"insert_text"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	values := make([]string, 0, len(payload.Items))
	for _, item := range payload.Items {
		values = append(values, item.Value)
		if !strings.HasSuffix(item.InsertText, " ") {
			t.Fatalf("item %q insert_text = %q, want trailing space", item.Value, item.InsertText)
		}
	}
	if !containsRuntimeCommandValue(values, "/help") {
		t.Fatalf("values = %v, want /help", values)
	}
	if !containsRuntimeCommandValue(values, "/models list") {
		t.Fatalf("values = %v, want /models list", values)
	}
	if containsRuntimeCommandValue(values, "/plan") {
		t.Fatalf("values contains placeholder /plan: %v", values)
	}
}

func TestCommandsRouteRejectsUnauthorizedRequest(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
	})

	req := httptest.NewRequest(http.MethodGet, "/commands", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func containsRuntimeCommandValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
