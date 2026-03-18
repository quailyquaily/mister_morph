package daemonruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubTopicStore struct {
	items   []TopicInfo
	deleted []string
}

func (s *stubTopicStore) ListTopics() []TopicInfo {
	return append([]TopicInfo(nil), s.items...)
}

func (s *stubTopicStore) DeleteTopic(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, item := range s.items {
		if item.ID == id {
			s.deleted = append(s.deleted, id)
			return true
		}
	}
	return false
}

func TestTasksRouteFiltersByTopicID(t *testing.T) {
	store := NewMemoryStore(10)
	store.Upsert(TaskInfo{
		ID:        "task_a",
		Status:    TaskQueued,
		Task:      "alpha",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
		TopicID:   "topic_a",
	})
	store.Upsert(TaskInfo{
		ID:        "task_b",
		Status:    TaskQueued,
		Task:      "beta",
		Model:     "gpt-5.2",
		Timeout:   "10m0s",
		CreatedAt: time.Date(2026, 3, 15, 10, 1, 0, 0, time.UTC),
		TopicID:   "topic_b",
	})

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:       "console",
		AuthToken:  "token",
		TaskReader: store,
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks?topic_id=topic_b&limit=20", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Items []TaskInfo `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(payload.Items))
	}
	if payload.Items[0].ID != "task_b" {
		t.Fatalf("items[0].ID = %q, want task_b", payload.Items[0].ID)
	}
}

func TestTopicsRoutesListAndDelete(t *testing.T) {
	topics := &stubTopicStore{
		items: []TopicInfo{
			{
				ID:        "topic_a",
				Title:     "Alpha",
				CreatedAt: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
			},
		},
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:         "console",
		AuthToken:    "token",
		TopicReader:  topics,
		TopicDeleter: topics,
	})

	t.Run("list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/topics", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			Items []TopicInfo `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if len(payload.Items) != 1 || payload.Items[0].ID != "topic_a" {
			t.Fatalf("payload.Items = %+v, want topic_a", payload.Items)
		}
	})

	t.Run("delete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/topics/topic_a", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusNoContent, rec.Body.String())
		}
		if len(topics.deleted) != 1 || topics.deleted[0] != "topic_a" {
			t.Fatalf("deleted = %+v, want [topic_a]", topics.deleted)
		}
	})
}

func TestTasksRouteSubmitReturnsTopicID(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token",
		Submit: func(_ context.Context, req SubmitTaskRequest) (SubmitTaskResponse, error) {
			if strings.TrimSpace(req.Task) == "" {
				t.Fatalf("Submit received empty task")
			}
			return SubmitTaskResponse{
				ID:      "task_1",
				Status:  TaskQueued,
				TopicID: "topic_new",
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"task":"hello"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload SubmitTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.TopicID != "topic_new" {
		t.Fatalf("payload.TopicID = %q, want topic_new", payload.TopicID)
	}
}
