package daemonruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	body := strings.NewReader(`{"tasks":[{"id":"one-off","title":"Queue review","at":"2026-05-18 09:30","tz":"Asia/Tokyo","content":"Check the queue"},{"id":"weekly","enabled":false,"cron":"0 10 * * 1","tz":"UTC","content":"Prepare weekly report","chat_id":"tg:-100","mention":"[Alice](tg:alice)"}]}`)
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
			Enabled *bool  `json:"enabled"`
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
	if payload.Tasks[1].Enabled == nil || *payload.Tasks[1].Enabled {
		t.Fatalf("expected weekly task to round-trip enabled=false, got %#v", payload.Tasks[1].Enabled)
	}
}

func TestTodoTasksRouteRunTriggersTaskByID(t *testing.T) {
	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
	})
	viper.Set("file_state_dir", stateDir)

	var got cronstore.Task
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
		CronRun: func(_ context.Context, task cronstore.Task) error {
			got = task
			return nil
		},
	})

	body := strings.NewReader(`{"tasks":[{"id":"weekly","cron":"0 10 * * 1","content":"Prepare weekly report"}]}`)
	putReq := httptest.NewRequest(http.MethodPut, "/todo/tasks", body)
	putReq.Header.Set("Authorization", "Bearer token")
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected PUT status 200, got %d (%s)", putRec.Code, putRec.Body.String())
	}

	runReq := httptest.NewRequest(http.MethodPost, "/todo/tasks/weekly/run", nil)
	runReq.Header.Set("Authorization", "Bearer token")
	runRec := httptest.NewRecorder()
	mux.ServeHTTP(runRec, runReq)
	if runRec.Code != http.StatusAccepted {
		t.Fatalf("expected POST status 202, got %d (%s)", runRec.Code, runRec.Body.String())
	}
	if got.ID != "weekly" || got.Content != "Prepare weekly report" {
		t.Fatalf("CronRun got %#v", got)
	}
}

func TestTodoTasksRouteRunReturnsNotFound(t *testing.T) {
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
		CronRun: func(_ context.Context, _ cronstore.Task) error {
			t.Fatal("CronRun should not be called")
			return nil
		},
	})

	runReq := httptest.NewRequest(http.MethodPost, "/todo/tasks/missing/run", nil)
	runReq.Header.Set("Authorization", "Bearer token")
	runRec := httptest.NewRecorder()
	mux.ServeHTTP(runRec, runReq)
	if runRec.Code != http.StatusNotFound {
		t.Fatalf("expected POST status 404, got %d (%s)", runRec.Code, runRec.Body.String())
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

func TestTodoTasksRouteReturnsHeartbeatSystemTask(t *testing.T) {
	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
	})
	viper.Set("file_state_dir", stateDir)

	settings := viper.New()
	settings.Set("cron.enabled", true)
	settings.Set("heartbeat.enabled", true)
	settings.Set("heartbeat.interval", 30*time.Minute)

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:                "serve",
		AuthToken:           "token",
		AgentSettingsReader: func() *viper.Viper { return settings },
	})

	req := httptest.NewRequest(http.MethodGet, "/todo/tasks", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected GET status 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		HeartbeatEnabled bool `json:"heartbeat_enabled"`
		SystemTasks      []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Cron  string `json:"cron"`
		} `json:"system_tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !payload.HeartbeatEnabled {
		t.Fatalf("heartbeat_enabled = false, want true")
	}
	if len(payload.SystemTasks) != 1 {
		t.Fatalf("system_tasks len = %d, want 1", len(payload.SystemTasks))
	}
	got := payload.SystemTasks[0]
	if got.ID != cronstore.HeartbeatTaskID || got.Title != "Heartbeat" || got.Cron != "*/30 * * * *" {
		t.Fatalf("unexpected heartbeat system task: %#v", got)
	}
}

func TestTodoTasksRouteReturnsHeartbeatDisabled(t *testing.T) {
	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
	})
	viper.Set("file_state_dir", stateDir)

	settings := viper.New()
	settings.Set("cron.enabled", true)
	settings.Set("heartbeat.enabled", false)

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:                "serve",
		AuthToken:           "token",
		AgentSettingsReader: func() *viper.Viper { return settings },
	})

	req := httptest.NewRequest(http.MethodGet, "/todo/tasks", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected GET status 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		HeartbeatEnabled bool             `json:"heartbeat_enabled"`
		SystemTasks      []cronstore.Task `json:"system_tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if payload.HeartbeatEnabled {
		t.Fatalf("heartbeat_enabled = true, want false")
	}
	if len(payload.SystemTasks) != 0 {
		t.Fatalf("system_tasks len = %d, want 0", len(payload.SystemTasks))
	}
}
