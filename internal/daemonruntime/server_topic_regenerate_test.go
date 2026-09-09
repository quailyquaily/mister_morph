package daemonruntime

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTopicRegenerateRoute(t *testing.T) {
	for _, tt := range []struct {
		name, method, id, token string
		fail                    error
		unavailable             bool
		status                  int
	}{
		{"success", "POST", "topic_a", "token", nil, false, 200},
		{"auth", "POST", "topic_a", "wrong", nil, false, 401},
		{"method", "GET", "topic_a", "token", nil, false, 405},
		{"missing", "POST", "missing", "token", nil, false, 404},
		{"reserved", "POST", "default", "token", nil, false, 400},
		{"unavailable", "POST", "topic_a", "token", nil, true, 503},
		{"empty", "POST", "topic_a", "token", BadRequest("no conversation"), false, 400},
		{"conflict", "POST", "topic_a", "token", ErrTopicTitleChanged, false, 409},
		{"busy", "POST", "topic_a", "token", ErrTopicTitleBusy, false, 409},
		{"failed", "POST", "topic_a", "token", errors.New("offline"), false, 503},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, _ := NewConsoleFileStore(ConsoleFileStoreOptions{})
			if err := store.Upsert(TaskInfo{ID: "task", TopicID: "topic_a", Task: "hello"}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			opts := RoutesOptions{AuthToken: "token", TaskTopic: TaskTopicRoutes{TopicReader: store}}
			if !tt.unavailable {
				opts.TaskTopic.RegenerateTopicTitle = func(_ context.Context, id string) (TopicInfo, error) {
					calls++
					return TopicInfo{ID: id, Title: "new name", Icon: "code"}, tt.fail
				}
			}
			req := httptest.NewRequest(tt.method, "/topics/"+tt.id+"/regenerate-title", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			rec := httptest.NewRecorder()
			NewHandler(opts).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if tt.status == 200 && !strings.Contains(rec.Body.String(), `"icon":"code"`) {
				t.Fatal(rec.Body.String())
			}
			if tt.status < 500 && tt.status != 200 && tt.fail == nil && calls != 0 {
				t.Fatal("unexpected generation")
			}
		})
	}
}
