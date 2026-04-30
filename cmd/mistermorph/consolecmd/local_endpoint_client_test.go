package consolecmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func TestInProcessRuntimeEndpointClientHealth(t *testing.T) {
	handler := daemonruntime.NewHandler(daemonruntime.RoutesOptions{
		Mode:      "console",
		AgentName: "Morph",
		AuthToken: "dev-token",
		Submit: func(context.Context, daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
			return daemonruntime.SubmitTaskResponse{}, nil
		},
		HealthEnabled: true,
	})
	client := newInProcessRuntimeEndpointClient(
		func() http.Handler { return handler },
		func() string { return "dev-token" },
		func() bool { return true },
	)

	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Mode != "console" {
		t.Fatalf("Health().Mode = %q, want %q", health.Mode, "console")
	}
	if health.AgentName != "Morph" {
		t.Fatalf("Health().AgentName = %q, want %q", health.AgentName, "Morph")
	}
	if !health.CanSubmit {
		t.Fatal("Health().CanSubmit = false, want true")
	}
	if health.InstanceID == "" {
		t.Fatal("Health().InstanceID is empty")
	}
}

func TestInProcessRuntimeEndpointClientProxyOverview(t *testing.T) {
	handler := daemonruntime.NewHandler(daemonruntime.RoutesOptions{
		Mode:          "console",
		AuthToken:     "dev-token",
		HealthEnabled: true,
	})
	client := newInProcessRuntimeEndpointClient(
		func() http.Handler { return handler },
		func() string { return "dev-token" },
		func() bool { return true },
	)

	status, raw, err := client.Proxy(context.Background(), http.MethodGet, "/overview", nil)
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("Proxy() status = %d, want %d (%s)", status, http.StatusOK, string(raw))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["mode"] != "console" {
		t.Fatalf("payload.mode = %#v, want %q", payload["mode"], "console")
	}
}

func TestInProcessRuntimeEndpointClientHealthOverridesSubmitCapability(t *testing.T) {
	handler := daemonruntime.NewHandler(daemonruntime.RoutesOptions{
		Mode:          "console",
		AuthToken:     "dev-token",
		HealthEnabled: true,
		Submit: func(context.Context, daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
			return daemonruntime.SubmitTaskResponse{}, nil
		},
	})
	client := newInProcessRuntimeEndpointClient(
		func() http.Handler { return handler },
		func() string { return "dev-token" },
		func() bool { return false },
	)

	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.CanSubmit {
		t.Fatal("Health().CanSubmit = true, want false")
	}
}

func TestInProcessRuntimeEndpointClientProxyEmptyPostBodyDoesNotPanic(t *testing.T) {
	handler := daemonruntime.NewHandler(daemonruntime.RoutesOptions{
		Mode:      "console",
		AuthToken: "dev-token",
		Submit: func(context.Context, daemonruntime.SubmitTaskRequest) (daemonruntime.SubmitTaskResponse, error) {
			return daemonruntime.SubmitTaskResponse{}, nil
		},
	})
	client := newInProcessRuntimeEndpointClient(
		func() http.Handler { return handler },
		func() string { return "dev-token" },
		func() bool { return true },
	)

	status, raw, err := client.Proxy(context.Background(), http.MethodPost, "/tasks", nil)
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("Proxy() status = %d, want %d (%s)", status, http.StatusBadRequest, string(raw))
	}
	if !strings.Contains(string(raw), "invalid json") {
		t.Fatalf("Proxy() body = %q, want invalid json", string(raw))
	}
}

func TestInProcessRuntimeEndpointClientDownloadReturnsAfterHeaders(t *testing.T) {
	continueWrite := make(chan struct{})
	authSeen := make(chan string, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		<-continueWrite
		_, _ = w.Write([]byte("streamed\n"))
	})
	client := newInProcessRuntimeEndpointClient(
		func() http.Handler { return handler },
		func() string { return "dev-token" },
		func() bool { return true },
	)

	type result struct {
		download runtimeEndpointDownload
		err      error
	}
	done := make(chan result, 1)
	go func() {
		download, err := client.Download(context.Background(), "/files/download")
		done <- result{download: download, err: err}
	}()

	var res result
	select {
	case res = <-done:
	case <-time.After(500 * time.Millisecond):
		close(continueWrite)
		t.Fatal("Download() did not return after headers were written")
	}
	if res.err != nil {
		close(continueWrite)
		t.Fatalf("Download() error = %v", res.err)
	}
	if got := <-authSeen; got != "Bearer dev-token" {
		close(continueWrite)
		t.Fatalf("Authorization = %q, want bearer token", got)
	}
	defer res.download.Body.Close()
	if res.download.Status != http.StatusOK {
		close(continueWrite)
		t.Fatalf("status = %d, want %d", res.download.Status, http.StatusOK)
	}
	if got := res.download.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		close(continueWrite)
		t.Fatalf("Content-Type = %q", got)
	}

	close(continueWrite)
	raw, err := io.ReadAll(res.download.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(raw) != "streamed\n" {
		t.Fatalf("body = %q", string(raw))
	}
}
