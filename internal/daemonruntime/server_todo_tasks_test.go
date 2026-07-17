package daemonruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/chatinfo"
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
	chatStore := chatinfo.NewStore(stateDir + "/contacts")
	if err := chatStore.Write(context.Background(), []chatinfo.Info{
		{
			ChatID:    "tg:-100",
			Platform:  "telegram",
			Type:      "supergroup",
			Name:      "Project Room",
			FetchedAt: time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, 7, 10, 1, 0, 0, 0, time.UTC),
		},
		{
			ChatID:    "tg:-200",
			Platform:  "telegram",
			Type:      "supergroup",
			Name:      "",
			FetchedAt: time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, 7, 10, 1, 0, 0, 0, time.UTC),
		},
	}); err != nil {
		t.Fatalf("seed chat profile: %v", err)
	}

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		Mode:      "serve",
		AuthToken: "token",
	})

	body := strings.NewReader(`{"tasks":[{"id":"one-off","title":"Queue review","at":"2026-05-18 09:30","tz":"Asia/Tokyo","content":"Check the queue"},{"id":"weekly","enabled":false,"cron":"0 10 * * 1","tz":"UTC","content":"Prepare weekly report","chat_id":"tg:-100","mention":"[Alice](tg:alice)","llm_profile":"batch"}]}`)
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
			ID         string `json:"id"`
			Title      string `json:"title"`
			At         string `json:"at"`
			Cron       string `json:"cron"`
			TZ         string `json:"tz"`
			Content    string `json:"content"`
			ChatID     string `json:"chat_id"`
			Mention    string `json:"mention"`
			LLMProfile string `json:"llm_profile"`
			Enabled    *bool  `json:"enabled"`
		} `json:"tasks"`
		ChatOptions []struct {
			ChatID   string `json:"chat_id"`
			Platform string `json:"platform"`
			Type     string `json:"type"`
			Name     string `json:"name"`
		} `json:"chat_options"`
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
	if payload.Tasks[1].LLMProfile != "batch" {
		t.Fatalf("unexpected llm profile: %#v", payload.Tasks[1])
	}
	if payload.Tasks[1].Enabled == nil || *payload.Tasks[1].Enabled {
		t.Fatalf("expected weekly task to round-trip enabled=false, got %#v", payload.Tasks[1].Enabled)
	}
	if len(payload.ChatOptions) != 1 || payload.ChatOptions[0].ChatID != "tg:-100" || payload.ChatOptions[0].Name != "Project Room" {
		t.Fatalf("unexpected chat options: %#v", payload.ChatOptions)
	}
}

func TestTodoTasksRouteFetchesChatOptionsFromActiveContacts(t *testing.T) {
	stateDir := t.TempDir()
	oldStateDir := viper.GetString("file_state_dir")
	t.Cleanup(func() {
		viper.Set("file_state_dir", oldStateDir)
	})
	viper.Set("file_state_dir", stateDir)

	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.info" {
			t.Fatalf("unexpected slack path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-test" {
			t.Fatalf("unexpected slack authorization: %q", got)
		}
		if got := r.URL.Query().Get("channel"); got != "C999" {
			t.Fatalf("unexpected slack channel: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"channel": map[string]any{
				"id":         "C999",
				"name":       "ops-room",
				"is_channel": true,
			},
		})
	}))
	defer slackServer.Close()
	if err := contacts.NewFileStore(stateDir+"/contacts").PutContact(context.Background(), contacts.Contact{
		ContactID:       "slack:T111:U222",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelSlack,
		SlackTeamID:     "T111",
		SlackChannelIDs: []string{"C999"},
	}); err != nil {
		t.Fatalf("seed active contact: %v", err)
	}

	settings := viper.New()
	settings.Set("slack.bot_token", "xoxb-test")
	settings.Set("slack.base_url", slackServer.URL)

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
		ChatOptions []struct {
			ChatID   string `json:"chat_id"`
			Platform string `json:"platform"`
			Type     string `json:"type"`
			Name     string `json:"name"`
		} `json:"chat_options"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(payload.ChatOptions) != 1 {
		t.Fatalf("chat_options len = %d, want 1: %#v", len(payload.ChatOptions), payload.ChatOptions)
	}
	got := payload.ChatOptions[0]
	if got.ChatID != "slack:T111:C999" || got.Platform != "slack" || got.Type != "channel" || got.Name != "ops-room" {
		t.Fatalf("unexpected chat option: %#v", got)
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
