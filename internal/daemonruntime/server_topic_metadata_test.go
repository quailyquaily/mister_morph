package daemonruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTopicMetadataRouteGet(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", TaskTopic: TaskTopicRoutes{TopicMetadata: func(_ context.Context, topicID string) (TopicMetadata, error) {
			if topicID != "topic_a" {
				t.Fatalf("topicID = %q, want %q", topicID, "topic_a")
			}
			return TopicMetadata{
				TopicID:         "topic_a",
				ConversationKey: "console:topic_a",
				Workspace: TopicMetadataWorkspace{
					WorkspaceDir: "/repo/project",
				},
				Context: TopicMetadataContext{
					Available:           true,
					Model:               "gpt-5.5",
					NormalizedModel:     "gpt-5.5",
					ContextWindowTokens: 1050000,
					ContextWindowSource: "builtin",
					UsedInputTokens:     105000,
					CachedInputTokens:   50000,
					UsageRatio:          0.1,
					LastRunID:           "task_1",
					UpdatedAt:           "2026-05-22T00:00:00Z",
				},
			}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/topic/topic_a/metadata", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload TopicMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Workspace.WorkspaceDir != "/repo/project" {
		t.Fatalf("workspace_dir = %q, want %q", payload.Workspace.WorkspaceDir, "/repo/project")
	}
	if !payload.Context.Available {
		t.Fatalf("context.available = false, want true")
	}
	if payload.Context.UsedInputTokens != 105000 {
		t.Fatalf("used_input_tokens = %d, want 105000", payload.Context.UsedInputTokens)
	}
	if payload.Context.LastRunID != "task_1" {
		t.Fatalf("last_run_id = %q, want task_1", payload.Context.LastRunID)
	}
}

func TestTopicMetadataRouteGetWithoutWorkspaceOrContext(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken: "token", TaskTopic: TaskTopicRoutes{TopicMetadata: func(_ context.Context, topicID string) (TopicMetadata, error) {
			return TopicMetadata{
				TopicID: topicID,
				Workspace: TopicMetadataWorkspace{
					WorkspaceDir: "",
				},
				Context: TopicMetadataContext{Available: false},
			}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/topic/topic_empty/metadata", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload TopicMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Workspace.WorkspaceDir != "" {
		t.Fatalf("workspace_dir = %q, want empty", payload.Workspace.WorkspaceDir)
	}
	if payload.Context.Available {
		t.Fatalf("context.available = true, want false")
	}
}

func TestTopicMetadataRouteUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{AuthToken: "token"})

	req := httptest.NewRequest(http.MethodGet, "/topic/topic_a/metadata", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestParseTopicMetadataPath(t *testing.T) {
	got, ok := parseTopicMetadataPath("/topic/topic%2Fa/metadata")
	if !ok {
		t.Fatalf("parseTopicMetadataPath() ok = false, want true")
	}
	if got != "topic/a" {
		t.Fatalf("topicID = %q, want topic/a", got)
	}
	if _, ok := parseTopicMetadataPath("/topic//metadata"); ok {
		t.Fatalf("parseTopicMetadataPath() ok = true for missing topic id")
	}
	if _, ok := parseTopicMetadataPath("/topic/a/b/metadata"); ok {
		t.Fatalf("parseTopicMetadataPath() ok = true for nested path")
	}
}
