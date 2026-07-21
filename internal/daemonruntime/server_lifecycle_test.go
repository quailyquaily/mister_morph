package daemonruntime

import (
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

func TestDaemonBodyReadTimeout(t *testing.T) {
	for _, test := range []struct {
		name string
		req  *http.Request
		want time.Duration
	}{
		{name: "ordinary body", req: httptest.NewRequest(http.MethodPost, "/tasks", nil), want: serverpolicy.BodyReadTimeout},
		{name: "upload", req: httptest.NewRequest(http.MethodPost, "/files/upload", nil), want: serverpolicy.UploadBodyReadTimeout},
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
