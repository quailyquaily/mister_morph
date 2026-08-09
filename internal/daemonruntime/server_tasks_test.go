package daemonruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pagination"
)

type stubTopicStore struct {
	items     []TopicInfo
	deleted   []string
	deleteErr error
	requested TopicListOptions
}

func (s *stubTopicStore) ListTopicsPage(opts TopicListOptions) []TopicInfo {
	s.requested = opts
	return append([]TopicInfo(nil), s.items...)
}

func (s *stubTopicStore) GetTopic(id string) (*TopicInfo, bool) {
	id = strings.TrimSpace(id)
	for _, item := range s.items {
		if item.ID == id {
			copy := item
			return &copy, true
		}
	}
	return nil, false
}

func (s *stubTopicStore) DeleteTopic(id string) (bool, error) {
	if s.deleteErr != nil {
		return false, s.deleteErr
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	for _, item := range s.items {
		if item.ID == id {
			s.deleted = append(s.deleted, id)
			return true, nil
		}
	}
	return false, nil
}

func TestTopicsRouteUsesLimitAndCursor(t *testing.T) {
	store := &stubTopicStore{items: []TopicInfo{
		{ID: "topic_3", UpdatedAt: time.Date(2026, 3, 15, 10, 3, 0, 0, time.UTC)},
		{ID: "topic_2", UpdatedAt: time.Date(2026, 3, 15, 10, 2, 0, 0, time.UTC)},
		{ID: "topic_1", UpdatedAt: time.Date(2026, 3, 15, 10, 1, 0, 0, time.UTC)},
	}}
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken: "token",
		TaskTopic: TaskTopicRoutes{TopicReader: store},
	})

	req := httptest.NewRequest(http.MethodGet, "/topics?limit=2", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var payload pagination.Page[TopicInfo]
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if store.requested.Limit != 3 || store.requested.Cursor != "" {
		t.Fatalf("requested options = %+v, want limit+1", store.requested)
	}
	if len(payload.Items) != 2 || !payload.HasNext || payload.NextCursor == "" {
		t.Fatalf("payload = %+v", payload)
	}
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
		Mode:      "console",
		AuthToken: "token",
		TaskTopic: TaskTopicRoutes{
			TaskReader: store,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks?topic_id=topic_b&limit=20", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload pagination.Page[TaskInfo]
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

func TestTasksRouteCursorPagination(t *testing.T) {
	store := NewMemoryStore(10)
	for _, item := range []TaskInfo{
		{
			ID:        "task_3",
			Status:    TaskDone,
			Task:      "three",
			Model:     "gpt-5.2",
			Timeout:   "10m0s",
			CreatedAt: time.Date(2026, 3, 15, 10, 2, 0, 0, time.UTC),
		},
		{
			ID:        "task_2",
			Status:    TaskDone,
			Task:      "two",
			Model:     "gpt-5.2",
			Timeout:   "10m0s",
			CreatedAt: time.Date(2026, 3, 15, 10, 1, 0, 0, time.UTC),
		},
		{
			ID:        "task_1",
			Status:    TaskDone,
			Task:      "one",
			Model:     "gpt-5.2",
			Timeout:   "10m0s",
			CreatedAt: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
		},
	} {
		store.Upsert(item)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", TaskTopic: TaskTopicRoutes{TaskReader: store},
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks?limit=2", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first page status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var first pagination.Page[TaskInfo]
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("json.Unmarshal(first) error = %v", err)
	}
	if len(first.Items) != 2 {
		t.Fatalf("len(first.Items) = %d, want 2", len(first.Items))
	}
	if !first.HasNext || strings.TrimSpace(first.NextCursor) == "" {
		t.Fatalf("first page missing next cursor: %+v", first)
	}
	if first.Items[0].ID != "task_3" || first.Items[1].ID != "task_2" {
		t.Fatalf("first.Items = %+v, want [task_3 task_2]", first.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/tasks?limit=2&cursor="+first.NextCursor, nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("second page status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var second pagination.Page[TaskInfo]
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("json.Unmarshal(second) error = %v", err)
	}
	if len(second.Items) != 1 {
		t.Fatalf("len(second.Items) = %d, want 1", len(second.Items))
	}
	if second.Items[0].ID != "task_1" {
		t.Fatalf("second.Items[0].ID = %q, want task_1", second.Items[0].ID)
	}
	if second.HasNext {
		t.Fatalf("second.HasNext = true, want false")
	}
}

func TestTasksRouteRejectsInvalidCursor(t *testing.T) {
	store := NewMemoryStore(10)
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", TaskTopic: TaskTopicRoutes{TaskReader: store},
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks?cursor=not-a-cursor", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
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
		Mode:      "console",
		AuthToken: "token", TaskTopic: TaskTopicRoutes{TopicReader: topics, TopicDeleter: topics},
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

	t.Run("get", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/topics/topic_a", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload TopicInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if payload.ID != "topic_a" || payload.Title != "Alpha" {
			t.Fatalf("payload = %+v, want topic_a", payload)
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

func TestTopicsRouteDeleteDistinguishesNotFoundAndPersistenceFailure(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		mux := http.NewServeMux()
		RegisterRoutes(mux, RoutesOptions{
			Mode:      "console",
			AuthToken: "token", TaskTopic: TaskTopicRoutes{TopicDeleter: &stubTopicStore{}},
		})
		req := httptest.NewRequest(http.MethodDelete, "/topics/missing", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("persistence failure", func(t *testing.T) {
		mux := http.NewServeMux()
		RegisterRoutes(mux, RoutesOptions{
			Mode:      "console",
			AuthToken: "token", TaskTopic: TaskTopicRoutes{TopicDeleter: &stubTopicStore{
				deleteErr: errors.New("journal append failed"),
			}},
		})
		req := httptest.NewRequest(http.MethodDelete, "/topics/topic_a", nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
		}
	})
}

func TestTasksRouteSubmitReturnsTopicID(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", TaskTopic: TaskTopicRoutes{Submit: func(_ context.Context, req SubmitTaskRequest) (SubmitTaskResponse, error) {
			if strings.TrimSpace(req.Task) == "" {
				t.Fatalf("Submit received empty task")
			}
			if req.WorkspaceDir != "/repo" {
				t.Fatalf("Submit WorkspaceDir = %q, want /repo", req.WorkspaceDir)
			}
			if req.LLMProfile != "cheap" {
				t.Fatalf("Submit LLMProfile = %q, want cheap", req.LLMProfile)
			}
			if len(req.FileReferences) != 2 {
				t.Fatalf("Submit FileReferences = %#v, want 2 items", req.FileReferences)
			}
			if req.FileReferences[0] != (FileReference{DirName: "workspace_dir", Path: "report-a.pdf"}) {
				t.Fatalf("Submit FileReferences[0] = %#v", req.FileReferences[0])
			}
			if req.FileReferences[1] != (FileReference{DirName: "file_cache_dir", Path: "report-b.pdf"}) {
				t.Fatalf("Submit FileReferences[1] = %#v", req.FileReferences[1])
			}
			return SubmitTaskResponse{
				ID:      "task_1",
				Status:  TaskQueued,
				TopicID: "topic_new",
			}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"task":"hello","workspace_dir":"/repo","llm_profile":"cheap","file_references":[{"dir_name":"workspace_dir","path":"report-a.pdf"},{"dir_name":"file_cache_dir","path":"report-b.pdf"}]}`))
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

func TestStopRoutesCallStopHandler(t *testing.T) {
	var calls []StopTaskRequest
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "console",
		AuthToken: "token", TaskTopic: TaskTopicRoutes{Stop: func(_ context.Context, req StopTaskRequest) (StopTaskResponse, error) {
			calls = append(calls, req)
			return StopTaskResponse{
				Status:   "stopping",
				Found:    true,
				TaskID:   req.TaskID,
				TopicID:  req.TopicID,
				Progress: "计划 1/3",
			}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks/task_1/stop", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("task stop status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var taskPayload StopTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &taskPayload); err != nil {
		t.Fatalf("json.Unmarshal(task) error = %v", err)
	}
	if taskPayload.TaskID != "task_1" || taskPayload.Status != "stopping" || taskPayload.Progress != "计划 1/3" {
		t.Fatalf("task stop payload = %+v", taskPayload)
	}

	req = httptest.NewRequest(http.MethodPost, "/topics/topic_a/stop", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("topic stop status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var topicPayload StopTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &topicPayload); err != nil {
		t.Fatalf("json.Unmarshal(topic) error = %v", err)
	}
	if topicPayload.TopicID != "topic_a" || topicPayload.Status != "stopping" || topicPayload.Progress != "计划 1/3" {
		t.Fatalf("topic stop payload = %+v", topicPayload)
	}

	if len(calls) != 2 {
		t.Fatalf("calls len = %d, want 2", len(calls))
	}
	if calls[0].TaskID != "task_1" || calls[0].TopicID != "" {
		t.Fatalf("task stop call = %+v", calls[0])
	}
	if calls[1].TaskID != "" || calls[1].TopicID != "topic_a" {
		t.Fatalf("topic stop call = %+v", calls[1])
	}
}
