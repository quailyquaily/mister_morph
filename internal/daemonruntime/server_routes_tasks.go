package daemonruntime

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/taskdomain"
)

func (routes *routeRegistration) registerTaskRoutes() {
	mux := routes.mux
	opts := routes.options.TaskTopic
	authToken := routes.authToken
	reader := opts.TaskReader
	topicReader := opts.TopicReader
	topicDeleter := opts.TopicDeleter
	submit := opts.Submit
	stop := opts.Stop

	mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if reader == nil {
				http.Error(w, "task reader is unavailable", http.StatusServiceUnavailable)
				return
			}
			rawStatus := strings.TrimSpace(r.URL.Query().Get("status"))
			status, ok := taskdomain.ParseTaskStatus(rawStatus)
			if !ok {
				http.Error(w, "invalid status", http.StatusBadRequest)
				return
			}
			limit := taskListDefaultLimit
			if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
				parsed, err := strconv.Atoi(rawLimit)
				if err != nil || parsed <= 0 {
					http.Error(w, "invalid limit", http.StatusBadRequest)
					return
				}
				if parsed > taskListMaxLimit {
					http.Error(w, "invalid limit", http.StatusBadRequest)
					return
				}
				limit = parsed
			}
			cursorRaw := strings.TrimSpace(r.URL.Query().Get("cursor"))
			if _, ok := parseTaskListCursor(cursorRaw); !ok {
				http.Error(w, "invalid cursor", http.StatusBadRequest)
				return
			}
			items := reader.List(TaskListOptions{
				Status:  status,
				Limit:   limit + 1,
				TopicID: strings.TrimSpace(r.URL.Query().Get("topic_id")),
				Cursor:  cursorRaw,
			})
			nextCursor := ""
			hasNext := len(items) > limit
			if hasNext {
				items = items[:limit]
				if len(items) > 0 {
					nextCursor = TaskListCursorAfter(items[len(items)-1])
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(TaskListResponse{
				Items:      items,
				Limit:      limit,
				NextCursor: nextCursor,
				HasNext:    hasNext,
			})
			return

		case http.MethodPost:
			if submit == nil {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req SubmitTaskRequest
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			req.Task = strings.TrimSpace(req.Task)
			if req.Task == "" {
				http.Error(w, "missing task", http.StatusBadRequest)
				return
			}
			resp, err := submit(r.Context(), req)
			if err != nil {
				if msg, ok := badRequestMessage(err); ok {
					http.Error(w, msg, http.StatusBadRequest)
					return
				}
				http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	mux.HandleFunc("/topics", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if topicReader == nil {
				http.Error(w, "topic reader is unavailable", http.StatusServiceUnavailable)
				return
			}
			items := topicReader.ListTopics()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	mux.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		suffix := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/tasks/"))
		if suffix == "" {
			http.Error(w, "missing task_id", http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(suffix, "/stop") {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if stop == nil {
				http.Error(w, "stop is unavailable", http.StatusServiceUnavailable)
				return
			}
			taskID := strings.TrimSpace(strings.TrimSuffix(suffix, "/stop"))
			if taskID == "" || strings.Contains(taskID, "/") {
				http.Error(w, "missing task_id", http.StatusBadRequest)
				return
			}
			resp, err := stop(r.Context(), StopTaskRequest{
				TaskID: taskID,
				Reason: "/stop",
			})
			if err != nil {
				if msg, ok := badRequestMessage(err); ok {
					http.Error(w, msg, http.StatusBadRequest)
					return
				}
				http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if reader == nil {
			http.Error(w, "task reader is unavailable", http.StatusServiceUnavailable)
			return
		}
		if strings.Contains(suffix, "/") {
			http.NotFound(w, r)
			return
		}
		info, ok := reader.Get(suffix)
		if !ok || info == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
	})

	mux.HandleFunc("/topics/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		suffix := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/topics/"))
		if suffix == "" {
			http.Error(w, "missing topic_id", http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(suffix, "/stop") {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if stop == nil {
				http.Error(w, "stop is unavailable", http.StatusServiceUnavailable)
				return
			}
			topicID := strings.TrimSpace(strings.TrimSuffix(suffix, "/stop"))
			if topicID == "" || strings.Contains(topicID, "/") {
				http.Error(w, "missing topic_id", http.StatusBadRequest)
				return
			}
			resp, err := stop(r.Context(), StopTaskRequest{
				TopicID: topicID,
				Reason:  "/stop",
			})
			if err != nil {
				if msg, ok := badRequestMessage(err); ok {
					http.Error(w, msg, http.StatusBadRequest)
					return
				}
				http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if topicDeleter == nil {
			http.Error(w, "topic delete is unavailable", http.StatusServiceUnavailable)
			return
		}
		id := suffix
		if strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		deleted, err := topicDeleter.DeleteTopic(id)
		if err != nil {
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		if !deleted {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
