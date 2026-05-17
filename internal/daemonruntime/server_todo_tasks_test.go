package daemonruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/spf13/viper"
)

func TestTodoTasksRouteRoundTrip(t *testing.T) {
	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
	})
	viper.Set("file_state_dir", stateDir)

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
	})

	body := strings.NewReader(`{"tasks":[{"id":"one-off","title":"Queue review","at":"2026-05-18 09:30","tz":"Asia/Tokyo","content":"Check the queue"},{"id":"weekly","cron":"0 10 * * 1","tz":"UTC","content":"Prepare weekly report","chat_id":"tg:-100","mention":"[Alice](tg:alice)"}]}`)
	putReq := httptest.NewRequest(http.MethodPut, "/todo/tasks", body)
	putReq.Header.Set("Authorization", "Bearer token")
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected PUT status 200, got %d (%s)", putRec.Code, putRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/todo/tasks", nil)
	getReq.Header.Set("Authorization", "Bearer token")
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected GET status 200, got %d (%s)", getRec.Code, getRec.Body.String())
	}

	var payload struct {
		Version   int `json:"version"`
		TaskCount int `json:"task_count"`
		Tasks     []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			At      string `json:"at"`
			Cron    string `json:"cron"`
			TZ      string `json:"tz"`
			Content string `json:"content"`
			ChatID  string `json:"chat_id"`
			Mention string `json:"mention"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if payload.Version != 1 || payload.TaskCount != 2 || len(payload.Tasks) != 2 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if payload.Tasks[0].ID != "one-off" || payload.Tasks[1].Cron != "0 10 * * 1" {
		t.Fatalf("unexpected tasks: %#v", payload.Tasks)
	}
	if payload.Tasks[0].Title != "Queue review" || payload.Tasks[1].Title != cronstore.DefaultTaskTitle {
		t.Fatalf("unexpected task titles: %#v", payload.Tasks)
	}
	if payload.Tasks[1].Mention != "[Alice](tg:alice)" {
		t.Fatalf("unexpected mention: %#v", payload.Tasks[1])
	}
}

func TestTodoTasksRouteRejectsInvalidSchedule(t *testing.T) {
	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
	})
	viper.Set("file_state_dir", stateDir)

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
	})

	body := strings.NewReader(`{"tasks":[{"id":"bad","at":"2026-05-18 09:30","cron":"0 10 * * 1","content":"Bad task"}]}`)
	req := httptest.NewRequest(http.MethodPut, "/todo/tasks", body)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must use exactly one of at or cron") {
		t.Fatalf("expected schedule validation error, got %q", rec.Body.String())
	}
}
