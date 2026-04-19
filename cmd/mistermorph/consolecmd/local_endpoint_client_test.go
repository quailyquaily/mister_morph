package consolecmd

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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
