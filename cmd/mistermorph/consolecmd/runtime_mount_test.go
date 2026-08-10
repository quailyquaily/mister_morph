package consolecmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/spf13/viper"
)

func TestConsoleMountsLocalRuntimeWithServerAuthToken(t *testing.T) {
	srv, runtime := newConsoleRuntimeMountTestServer("/", "runtime-token")
	runtime.handler = daemonruntime.NewHandler(daemonruntime.RoutesOptions{
		Mode:          "console",
		AgentName:     "Remote Morph",
		AuthToken:     "runtime-token",
		HealthEnabled: true,
		TaskTopic: daemonruntime.TaskTopicRoutes{
			Submit: func(_ context.Context, _ daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
				return daemonruntime.SubmitTaskResponse{ID: "task_remote", Status: daemonruntime.TaskQueued}, nil
			},
		},
	})
	handler := srv.handler()

	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, httptest.NewRequest(http.MethodGet, "/runtime/health", nil))
	if healthRec.Code != http.StatusOK || !strings.Contains(healthRec.Body.String(), `"mode":"console"`) {
		t.Fatalf("runtime health = %d %s, want console health", healthRec.Code, healthRec.Body.String())
	}

	consoleHealthRec := httptest.NewRecorder()
	handler.ServeHTTP(consoleHealthRec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if consoleHealthRec.Code != http.StatusOK || !strings.Contains(consoleHealthRec.Body.String(), `"mode":"ready"`) {
		t.Fatalf("console health = %d %s, want ready health", consoleHealthRec.Code, consoleHealthRec.Body.String())
	}

	unauthorizedRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRec, httptest.NewRequest(http.MethodGet, "/runtime/overview", nil))
	if unauthorizedRec.Code != http.StatusUnauthorized {
		t.Fatalf("runtime overview without token status = %d, want %d", unauthorizedRec.Code, http.StatusUnauthorized)
	}
	wrongTokenReq := httptest.NewRequest(http.MethodGet, "/runtime/overview", nil)
	wrongTokenReq.Header.Set("Authorization", "Bearer wrong-token")
	wrongTokenRec := httptest.NewRecorder()
	handler.ServeHTTP(wrongTokenRec, wrongTokenReq)
	if wrongTokenRec.Code != http.StatusUnauthorized {
		t.Fatalf("runtime overview with wrong token status = %d, want %d", wrongTokenRec.Code, http.StatusUnauthorized)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/runtime/overview", nil)
	authorizedReq.Header.Set("Authorization", "Bearer runtime-token")
	authorizedRec := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRec, authorizedReq)
	if authorizedRec.Code != http.StatusOK {
		t.Fatalf("runtime overview with token status = %d, want %d (%s)", authorizedRec.Code, http.StatusOK, authorizedRec.Body.String())
	}

	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	client := newDaemonTaskClient(upstream.URL+"/runtime", "runtime-token")
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("remote endpoint health: %v", err)
	}
	if health.Mode != "console" || health.AgentName != "Remote Morph" || !health.CanSubmit {
		t.Fatalf("remote endpoint health = %+v", health)
	}
	status, body, err := client.Proxy(context.Background(), http.MethodPost, "/tasks", []byte(`{"task":"hello"}`), "application/json")
	if err != nil {
		t.Fatalf("remote endpoint submit: %v", err)
	}
	if status != http.StatusOK || !strings.Contains(string(body), `"id":"task_remote"`) {
		t.Fatalf("remote endpoint submit = %d %s", status, body)
	}
}

func TestConsoleDoesNotMountLocalRuntimeWithoutConfiguredToken(t *testing.T) {
	srv, runtime := newConsoleRuntimeMountTestServer("/", "")
	srv.cfg.staticFS = fstest.MapFS{
		"index.html": {Data: []byte("console spa")},
	}
	runtime.handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runtime/health", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("runtime route without configured token status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestConsoleRuntimeMountUsesBasePathAndCurrentHandler(t *testing.T) {
	srv, runtime := newConsoleRuntimeMountTestServer("/morph", "runtime-token")
	runtime.handler = runtimeMountMarkerHandler("old")
	handler := srv.handler()

	assertConsoleRuntimeMountMarker(t, handler, "/morph/runtime/marker", "old")

	runtime.handlerMu.Lock()
	runtime.handler = runtimeMountMarkerHandler("new")
	runtime.handlerMu.Unlock()

	assertConsoleRuntimeMountMarker(t, handler, "/morph/runtime/marker", "new")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runtime/marker", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("runtime route outside base path status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func newConsoleRuntimeMountTestServer(basePath string, authToken string) (*server, *consoleLocalRuntime) {
	reader := viper.New()
	reader.Set("server.auth_token", authToken)
	runtime := &consoleLocalRuntime{
		generation: &consoleLocalRuntimeGeneration{reader: reader},
	}
	return &server{
		cfg:          serveConfig{basePath: basePath},
		localRuntime: runtime,
	}, runtime
}

func runtimeMountMarkerHandler(marker string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/marker" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(marker))
	})
}

func assertConsoleRuntimeMountMarker(t *testing.T, handler http.Handler, target string, want string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("GET %s = %d %q, want %d %q", target, rec.Code, rec.Body.String(), http.StatusOK, want)
	}
}
