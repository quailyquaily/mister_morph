package consolecmd

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	serverpolicy "github.com/quailyquaily/mistermorph/internal/httpserver"
)

func TestNewConsoleHTTPServerHasLifecycleTimeouts(t *testing.T) {
	srv := newConsoleHTTPServer(&server{cfg: serveConfig{listen: "127.0.0.1:0"}})
	if srv.ReadHeaderTimeout != serverpolicy.ReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, serverpolicy.ReadHeaderTimeout)
	}
	if srv.IdleTimeout != serverpolicy.IdleTimeout {
		t.Fatalf("IdleTimeout = %v, want %v", srv.IdleTimeout, serverpolicy.IdleTimeout)
	}
}

func TestConsoleBodyReadTimeout(t *testing.T) {
	apiPrefix := "/console/api"
	for _, test := range []struct {
		name string
		req  *http.Request
		want time.Duration
	}{
		{name: "ordinary body", req: httptest.NewRequest(http.MethodPost, apiPrefix+"/auth/login", nil), want: serverpolicy.BodyReadTimeout},
		{name: "upload proxy", req: httptest.NewRequest(http.MethodPost, apiPrefix+"/proxy?uri=%2Ffiles%2Fupload", nil), want: serverpolicy.UploadBodyReadTimeout},
		{name: "download", req: httptest.NewRequest(http.MethodGet, apiPrefix+"/proxy/download", nil)},
		{name: "stream", req: httptest.NewRequest(http.MethodGet, apiPrefix+"/stream/ws", nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := consoleBodyReadTimeout(test.req, apiPrefix); got != test.want {
				t.Fatalf("consoleBodyReadTimeout() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestConsoleServeStopsIdleHTTPServerWhenContextCanceled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := &server{cfg: serveConfig{listen: listener.Addr().String(), basePath: "/"}}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.serve(ctx, listener)
	}()

	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve() did not stop after context cancellation")
	}
}

func TestConsoleServeClosesActiveWebSocketsBeforeReturning(t *testing.T) {
	for _, test := range []struct {
		name string
		path func(ticket string) string
	}{
		{
			name: "task stream",
			path: func(ticket string) string {
				return "/api/stream/ws?ticket=" + ticket + "&task_id=task-1"
			},
		},
		{
			name: "notifications",
			path: func(ticket string) string {
				return "/api/notifications/ws?ticket=" + ticket
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			t.Cleanup(func() { _ = listener.Close() })

			tickets := newSessionStore("")
			ticket, _, err := tickets.Create(time.Minute)
			if err != nil {
				t.Fatalf("create ticket: %v", err)
			}
			srv := &server{
				cfg:           serveConfig{listen: listener.Addr().String(), basePath: "/"},
				streamTickets: tickets,
				localRuntime: &consoleLocalRuntime{
					streamHub:       newConsoleStreamHub(),
					notificationHub: newConsoleNotificationHub(),
				},
			}

			ctx, cancel := context.WithCancel(context.Background())
			serveDone := make(chan error, 1)
			go func() {
				serveDone <- srv.serve(ctx, listener)
			}()

			wsURL := "ws://" + listener.Addr().String() + test.path(ticket)
			header := http.Header{"Origin": []string{"http://" + listener.Addr().String()}}
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
			if err != nil {
				cancel()
				<-serveDone
				t.Fatalf("dial websocket: %v", err)
			}
			defer conn.Close()

			cancel()
			select {
			case err := <-serveDone:
				if err != nil {
					t.Fatalf("serve() error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("serve() did not close and join the active websocket handler")
			}

			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			_, _, err = conn.ReadMessage()
			if err == nil {
				t.Fatalf("websocket remained open after serve() returned: %v", err)
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				t.Fatalf("websocket remained open after serve() returned: %v", err)
			}
		})
	}
}
