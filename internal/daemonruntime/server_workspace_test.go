package daemonruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkspaceRouteGet(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceGet: func(_ context.Context, topicID string) (string, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return "/repo/project", nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/workspace?topic_id=topic_a", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["topic_id"] != "topic_a" {
		t.Fatalf("payload.topic_id = %#v, want %q", payload["topic_id"], "topic_a")
	}
	if payload["workspace_dir"] != "/repo/project" {
		t.Fatalf("payload.workspace_dir = %#v, want %q", payload["workspace_dir"], "/repo/project")
	}
}

func TestWorkspaceRoutePut(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspacePut: func(_ context.Context, topicID string, workspaceDir string) (string, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			if workspaceDir != "./repo" {
				t.Fatalf("workspaceDir = %q, want %q", workspaceDir, "./repo")
			}
			return "/repo/project", nil
		},
	})

	req := httptest.NewRequest(http.MethodPut, "/workspace", strings.NewReader(`{"topic_id":"topic_a","workspace_dir":"./repo"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["workspace_dir"] != "/repo/project" {
		t.Fatalf("payload.workspace_dir = %#v, want %q", payload["workspace_dir"], "/repo/project")
	}
}

func TestWorkspaceRouteDelete(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspaceDelete: func(_ context.Context, topicID string) error {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/workspace?topic_id=topic_a", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["workspace_dir"] != "" {
		t.Fatalf("payload.workspace_dir = %#v, want empty", payload["workspace_dir"])
	}
}

func TestWorkspaceRouteUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
	})

	req := httptest.NewRequest(http.MethodGet, "/workspace?topic_id=topic_a", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestWorkspaceRouteBadRequestErrors(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		WorkspacePut: func(_ context.Context, topicID string, workspaceDir string) (string, error) {
			return "", BadRequest("workspace dir does not exist")
		},
	})

	req := httptest.NewRequest(http.MethodPut, "/workspace", strings.NewReader(`{"topic_id":"topic_a","workspace_dir":"./missing"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "workspace dir does not exist" {
		t.Fatalf("body = %q, want %q", got, "workspace dir does not exist")
	}
}
