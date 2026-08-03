package consolecmd

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/spf13/viper"
)

func TestConsoleRoutesUseCapturedRuntimePaths(t *testing.T) {
	globalState := t.TempDir()
	globalCache := t.TempDir()
	viper.Set("file_state_dir", globalState)
	viper.Set("file_cache_dir", globalCache)
	t.Cleanup(viper.Reset)

	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "runtime-path.txt"), []byte("captured-state"), 0o600); err != nil {
		t.Fatalf("WriteFile(captured state) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalState, "runtime-path.txt"), []byte("global-state"), 0o600); err != nil {
		t.Fatalf("WriteFile(global state) error = %v", err)
	}

	runtime := newConsoleRuntimeBoundaryFixture(t, stateDir, cacheDir)
	req := httptest.NewRequest(http.MethodGet, "/files/download?dir_name=file_state_dir&path=runtime-path.txt", nil)
	req.Header.Set("Authorization", "Bearer "+runtime.currentAuthToken())
	rec := httptest.NewRecorder()
	runtime.currentHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d, body = %q", rec.Code, rec.Body.String())
	}
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll(download) error = %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "captured-state" {
		t.Fatalf("download body = %q, want captured runtime state", got)
	}
}

func TestConsoleReloadRejectsBootOnlyRuntimePathChanges(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value func(*testing.T) string
	}{
		{name: "state", field: "file_state_dir", value: func(t *testing.T) string { return t.TempDir() }},
		{name: "cache", field: "file_cache_dir", value: func(t *testing.T) string { return t.TempDir() }},
		{name: "journal", field: "journal.dir_name", value: func(*testing.T) string { return "next-journal" }},
		{name: "memory", field: "memory.dir_name", value: func(*testing.T) string { return "next-memory" }},
		{name: "contacts", field: "contacts.dir_name", value: func(*testing.T) string { return "next-contacts" }},
		{name: "tasks", field: "tasks.dir_name", value: func(*testing.T) string { return "next-tasks" }},
		{name: "workspace", field: "file_state_dir", value: func(t *testing.T) string { return t.TempDir() }},
		{name: "checkpoint", field: "file_state_dir", value: func(t *testing.T) string { return t.TempDir() }},
		{name: "audit", field: "guard.audit.jsonl_path", value: func(t *testing.T) string { return filepath.Join(t.TempDir(), "next-audit.jsonl") }},
		{name: "log", field: "logging.file.dir", value: func(t *testing.T) string { return t.TempDir() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			cacheDir := t.TempDir()
			runtime := newConsoleRuntimeBoundaryFixture(t, stateDir, cacheDir)
			oldGeneration := runtime.currentGeneration()
			oldStore := runtime.store
			oldWorkspaceStore := runtime.currentWorkspaceStore()

			next := consoleRuntimeBoundaryReader(stateDir, cacheDir)
			next.Set(tc.field, tc.value(t))
			err := runtime.ReloadAgentConfigFromReader(next)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "boot-only") {
				t.Fatalf("ReloadAgentConfigFromReader(%s) error = %v, want boot-only rejection", tc.field, err)
			}
			if runtime.currentGeneration() != oldGeneration {
				t.Fatal("rejected reload changed the active generation")
			}
			if runtime.store != oldStore {
				t.Fatal("rejected reload changed the task store")
			}
			if runtime.currentWorkspaceStore() != oldWorkspaceStore {
				t.Fatal("rejected reload changed the workspace store")
			}
		})
	}
}

func TestConsoleReloadRejectsInvalidDefaultWorkspaceWithoutReplacingGeneration(t *testing.T) {
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	runtime := newConsoleRuntimeBoundaryFixture(t, stateDir, cacheDir)
	oldGeneration := runtime.currentGeneration()

	next := consoleRuntimeBoundaryReader(stateDir, cacheDir)
	next.Set("workspace_dir", filepath.Join(t.TempDir(), "missing"))
	err := runtime.ReloadAgentConfigFromReader(next)
	if err == nil || !strings.Contains(err.Error(), "workspace dir does not exist") {
		t.Fatalf("ReloadAgentConfigFromReader() error = %v, want workspace validation error", err)
	}
	if runtime.currentGeneration() != oldGeneration {
		t.Fatal("invalid workspace reload replaced the active generation")
	}
}

func TestConsoleAdmissionKeepsGenerationStoreWorkspaceAndRoutesTogether(t *testing.T) {
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	runtime := newConsoleRuntimeBoundaryFixture(t, stateDir, cacheDir)
	oldGeneration := runtime.currentGeneration()
	oldWorkspaceStore := runtime.currentWorkspaceStore()
	oldRoutes := runtime.routesOptions(runtime.currentAuthToken())

	next := consoleRuntimeBoundaryReader(stateDir, cacheDir)
	next.Set("llm.model", "next-model")
	if err := runtime.ReloadAgentConfigFromReader(next); err != nil {
		t.Fatalf("ReloadAgentConfigFromReader() error = %v", err)
	}
	newGeneration := runtime.currentGeneration()
	newRoutes := runtime.routesOptions(runtime.currentAuthToken())
	if newGeneration == oldGeneration {
		t.Fatal("reload did not install a new generation")
	}
	if oldRoutes.TaskTopic.TaskReader != runtime.store || newRoutes.TaskTopic.TaskReader != runtime.store {
		t.Fatal("route generations do not share the boot-owned task store")
	}
	if runtime.currentWorkspaceStore() != oldWorkspaceStore {
		t.Fatal("reload replaced the boot-owned workspace store")
	}
	if oldRoutes.RuntimePaths != newRoutes.RuntimePaths || oldRoutes.RuntimePaths != oldGeneration.paths {
		t.Fatal("route generations do not share the boot-owned RuntimePaths")
	}
	if got := oldRoutes.AgentSettingsReader.GetString("llm.model"); got != "test-model" {
		t.Fatalf("old route reader model = %q, want test-model", got)
	}
	if got := newRoutes.AgentSettingsReader.GetString("llm.model"); got != "next-model" {
		t.Fatalf("new route reader model = %q, want next-model", got)
	}

	oldOverview, err := oldRoutes.Overview(context.Background())
	if err != nil {
		t.Fatalf("old Overview() error = %v", err)
	}
	newOverview, err := newRoutes.Overview(context.Background())
	if err != nil {
		t.Fatalf("new Overview() error = %v", err)
	}
	if got := overviewModel(oldOverview); got != "test-model" {
		t.Fatalf("old route overview model = %q, want test-model", got)
	}
	if got := overviewModel(newOverview); got != "next-model" {
		t.Fatalf("new route overview model = %q, want next-model", got)
	}

	bus := runtime.bus
	runtime.bus = nil
	defer func() { runtime.bus = bus }()
	if _, err := oldRoutes.TaskTopic.Submit(context.Background(), daemonruntime.SubmitTaskRequest{Task: "old-generation-admission"}); err == nil {
		t.Fatal("old route Submit() error = nil, want disabled bus error")
	}
	if _, err := newRoutes.TaskTopic.Submit(context.Background(), daemonruntime.SubmitTaskRequest{Task: "new-generation-admission"}); err == nil {
		t.Fatal("new route Submit() error = nil, want disabled bus error")
	}
	models := map[string]string{}
	for _, item := range runtime.store.List(daemonruntime.TaskListOptions{Limit: 20}) {
		models[item.Task] = item.Model
	}
	if got := models["old-generation-admission"]; got != "test-model" {
		t.Fatalf("old admission model = %q, want test-model", got)
	}
	if got := models["new-generation-admission"]; got != "next-model" {
		t.Fatalf("new admission model = %q, want next-model", got)
	}
}

func TestConsoleWorkspaceRoutesKeepTheirGenerationDefault(t *testing.T) {
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	oldWorkspaceDir := t.TempDir()
	newWorkspaceDir := t.TempDir()
	reader := consoleRuntimeBoundaryReader(stateDir, cacheDir)
	reader.Set("workspace_dir", oldWorkspaceDir)
	runtime, err := newConsoleLocalRuntime(serveConfig{}, reader)
	if err != nil {
		t.Fatalf("newConsoleLocalRuntime() error = %v", err)
	}
	t.Cleanup(runtime.Close)
	oldRoutes := runtime.routesOptions(runtime.currentAuthToken())

	next := consoleRuntimeBoundaryReader(stateDir, cacheDir)
	next.Set("workspace_dir", newWorkspaceDir)
	if err := runtime.ReloadAgentConfigFromReader(next); err != nil {
		t.Fatalf("ReloadAgentConfigFromReader() error = %v", err)
	}
	newRoutes := runtime.routesOptions(runtime.currentAuthToken())

	oldResolution, err := oldRoutes.Workspace.Get(context.Background(), "topic_a")
	if err != nil {
		t.Fatalf("old Workspace.Get() error = %v", err)
	}
	newResolution, err := newRoutes.Workspace.Get(context.Background(), "topic_a")
	if err != nil {
		t.Fatalf("new Workspace.Get() error = %v", err)
	}
	if oldResolution.WorkspaceDir != oldWorkspaceDir || oldResolution.Source != "default" {
		t.Fatalf("old workspace resolution = %#v", oldResolution)
	}
	if newResolution.WorkspaceDir != newWorkspaceDir || newResolution.Source != "default" {
		t.Fatalf("new workspace resolution = %#v", newResolution)
	}
}

func overviewModel(payload map[string]any) string {
	llmPayload, _ := payload["llm"].(map[string]any)
	model, _ := llmPayload["model"].(string)
	return model
}

func newConsoleRuntimeBoundaryFixture(t *testing.T, stateDir, cacheDir string) *consoleLocalRuntime {
	t.Helper()
	logger := slog.Default()
	runtime, err := newConsoleLocalRuntime(serveConfig{}, consoleRuntimeBoundaryReader(stateDir, cacheDir))
	if err != nil {
		t.Fatalf("newConsoleLocalRuntime() error = %v", err)
	}
	t.Cleanup(func() {
		runtime.Close()
		slog.SetDefault(logger)
	})
	return runtime
}

func consoleRuntimeBoundaryReader(stateDir, cacheDir string) *viper.Viper {
	reader := viper.New()
	configdefaults.Apply(reader)
	reader.Set("file_state_dir", stateDir)
	reader.Set("file_cache_dir", cacheDir)
	reader.Set("llm.provider", "openai")
	reader.Set("llm.model", "test-model")
	reader.Set("llm.api_key", "test-key")
	reader.Set("memory.enabled", false)
	reader.Set("heartbeat.enabled", false)
	reader.Set("cron.enabled", false)
	reader.Set("guard.enabled", false)
	reader.Set("tools.image_generate.enabled", false)
	reader.Set("tools.image_edit.enabled", false)
	reader.Set("server.auth_token", "runtime-boundary-token")
	return reader
}
