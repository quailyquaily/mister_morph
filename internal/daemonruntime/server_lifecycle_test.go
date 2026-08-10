package daemonruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	serverpolicy "github.com/quailyquaily/mistermorph/internal/httpserver"
)

func TestNewDaemonHTTPServerHasLifecycleTimeouts(t *testing.T) {
	srv := newDaemonHTTPServer(ServerOptions{Listen: "127.0.0.1:0"})
	if srv.ReadHeaderTimeout != serverpolicy.ReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, serverpolicy.ReadHeaderTimeout)
	}
	if srv.IdleTimeout != serverpolicy.IdleTimeout {
		t.Fatalf("IdleTimeout = %v, want %v", srv.IdleTimeout, serverpolicy.IdleTimeout)
	}
}

func TestDaemonHTTPServerMountsCanonicalAndLegacyRuntimePaths(t *testing.T) {
	srv := newDaemonHTTPServer(ServerOptions{Routes: RoutesOptions{
		Mode:          "telegram",
		AuthToken:     "runtime-token",
		HealthEnabled: true,
	}})

	for _, target := range []string{"/health", "/runtime/health"} {
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d (%s)", target, rec.Code, http.StatusOK, rec.Body.String())
		}
		var payload struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("GET %s decode: %v", target, err)
		}
		if payload.Mode != "telegram" {
			t.Fatalf("GET %s mode = %q, want telegram", target, payload.Mode)
		}
	}

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runtime/overview", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated canonical route status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req := httptest.NewRequest(http.MethodGet, "/runtime/overview", nil)
	req.Header.Set("Authorization", "Bearer runtime-token")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated canonical route status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestDaemonBodyReadTimeout(t *testing.T) {
	for _, test := range []struct {
		name string
		req  *http.Request
		want time.Duration
	}{
		{name: "ordinary body", req: httptest.NewRequest(http.MethodPost, "/tasks", nil), want: serverpolicy.BodyReadTimeout},
		{name: "upload", req: httptest.NewRequest(http.MethodPost, "/files/upload", nil), want: serverpolicy.UploadBodyReadTimeout},
		{name: "canonical upload", req: httptest.NewRequest(http.MethodPost, "/runtime/files/upload", nil), want: serverpolicy.UploadBodyReadTimeout},
		{name: "download", req: httptest.NewRequest(http.MethodGet, "/files/download", nil)},
		{name: "stream", req: httptest.NewRequest(http.MethodGet, "/stream", nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := daemonBodyReadTimeout(test.req); got != test.want {
				t.Fatalf("daemonBodyReadTimeout() = %v, want %v", got, test.want)
			}
		})
	}
}
