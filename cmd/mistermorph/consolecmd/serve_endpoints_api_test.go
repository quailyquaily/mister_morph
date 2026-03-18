package consolecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubRuntimeEndpointClient struct {
	health    runtimeEndpointHealth
	healthErr error

	proxyStatus int
	proxyRaw    []byte
	proxyErr    error

	lastMethod string
	lastPath   string
	lastBody   []byte
}

func (s *stubRuntimeEndpointClient) Health(_ context.Context) (runtimeEndpointHealth, error) {
	return s.health, s.healthErr
}

func (s *stubRuntimeEndpointClient) Proxy(_ context.Context, method, endpointPath string, body []byte) (int, []byte, error) {
	s.lastMethod = method
	s.lastPath = endpointPath
	s.lastBody = append([]byte(nil), body...)
	return s.proxyStatus, append([]byte(nil), s.proxyRaw...), s.proxyErr
}

func TestHandleEndpointsSnapshots(t *testing.T) {
	s := &server{
		endpoints: []runtimeEndpoint{
			{
				Ref:  "ep_a",
				Name: "Main",
				URL:  "http://127.0.0.1:8787",
				Client: &stubRuntimeEndpointClient{
					health: runtimeEndpointHealth{Mode: "serve", AgentName: "Morph", CanSubmit: true},
				},
			},
			{
				Ref:    "ep_b",
				Name:   "Backup",
				URL:    "http://127.0.0.1:8788",
				Client: &stubRuntimeEndpointClient{healthErr: errors.New("dial failed")},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/console/api/endpoints", nil)
	rec := httptest.NewRecorder()
	s.handleEndpoints(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Items []struct {
			Ref       string `json:"endpoint_ref"`
			Name      string `json:"name"`
			URL       string `json:"url"`
			Connected bool   `json:"connected"`
			AgentName string `json:"agent_name"`
			Mode      string `json:"mode"`
			CanSubmit bool   `json:"can_submit"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(payload.Items))
	}
	if payload.Items[0].Ref != "ep_a" || payload.Items[0].Name != "Main" || payload.Items[0].URL != "http://127.0.0.1:8787" || !payload.Items[0].Connected || payload.Items[0].AgentName != "Morph" || payload.Items[0].Mode != "serve" || !payload.Items[0].CanSubmit {
		t.Fatalf("item[0] mismatch: %+v", payload.Items[0])
	}
	if payload.Items[1].Ref != "ep_b" || payload.Items[1].Name != "Backup" || payload.Items[1].URL != "http://127.0.0.1:8788" || payload.Items[1].Connected {
		t.Fatalf("item[1] mismatch: %+v", payload.Items[1])
	}
}

func TestHandleEndpointsReturnsEmptyAgentNameWhenHealthDoesNotProvideIt(t *testing.T) {
	client := &stubRuntimeEndpointClient{
		health: runtimeEndpointHealth{Mode: "console", CanSubmit: true},
	}
	s := &server{
		endpoints: []runtimeEndpoint{
			{
				Ref:    "ep_a",
				Name:   "Main",
				URL:    "http://127.0.0.1:8787",
				Client: client,
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/console/api/endpoints", nil)
	rec := httptest.NewRecorder()
	s.handleEndpoints(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Items []struct {
			AgentName string `json:"agent_name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(payload.Items))
	}
	if payload.Items[0].AgentName != "" {
		t.Fatalf("item[0].AgentName = %q, want empty", payload.Items[0].AgentName)
	}
}

func TestHandleEndpointsDoesNotMapSubmitForSameInstanceReadOnlyEndpoint(t *testing.T) {
	s := &server{
		endpoints: []runtimeEndpoint{
			{
				Ref:    consoleLocalEndpointRef,
				Name:   "Console Local",
				URL:    "in-process://console-local",
				Client: &stubRuntimeEndpointClient{health: runtimeEndpointHealth{Mode: "console", CanSubmit: true, InstanceID: "inst_same"}},
			},
			{
				Ref:    "ep_main",
				Name:   "Main",
				URL:    "http://127.0.0.1:8787",
				Client: &stubRuntimeEndpointClient{health: runtimeEndpointHealth{Mode: "telegram", CanSubmit: false, InstanceID: "inst_same"}},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/console/api/endpoints", nil)
	rec := httptest.NewRecorder()
	s.handleEndpoints(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Items []struct {
			Ref               string `json:"endpoint_ref"`
			SubmitEndpointRef string `json:"submit_endpoint_ref"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(payload.Items))
	}
	if payload.Items[1].Ref != "ep_main" {
		t.Fatalf("item[1].Ref = %q, want %q", payload.Items[1].Ref, "ep_main")
	}
	if payload.Items[1].SubmitEndpointRef != "" {
		t.Fatalf("item[1].SubmitEndpointRef = %q, want empty", payload.Items[1].SubmitEndpointRef)
	}
}

func TestHandleProxyRoutesToSelectedEndpoint(t *testing.T) {
	client := &stubRuntimeEndpointClient{
		proxyStatus: http.StatusAccepted,
		proxyRaw:    []byte(`{"ok":true}`),
	}
	s := &server{
		endpointByRef: map[string]runtimeEndpoint{
			"ep_main": {
				Ref:    "ep_main",
				Name:   "Main",
				URL:    "http://127.0.0.1:8787",
				Client: client,
			},
		},
	}

	reqBody := []byte(`{"task":"ping"}`)
	req := httptest.NewRequest(http.MethodPost, "/console/api/proxy?endpoint=ep_main&uri=/tasks?wait=true", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	s.handleProxy(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if client.lastMethod != http.MethodPost {
		t.Fatalf("client method = %q, want %q", client.lastMethod, http.MethodPost)
	}
	if client.lastPath != "/tasks?wait=true" {
		t.Fatalf("client path = %q, want %q", client.lastPath, "/tasks?wait=true")
	}
	if string(client.lastBody) != string(reqBody) {
		t.Fatalf("client body = %q, want %q", string(client.lastBody), string(reqBody))
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("payload.ok = %#v, want true", payload["ok"])
	}
}
