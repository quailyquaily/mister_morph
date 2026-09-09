package consolecmd

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type topicTitleRoundTripFunc func(*http.Request) (*http.Response, error)

func (f topicTitleRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestTopicRegenerateProxyUsesLLMTimeoutInsteadOfProxyTimeout(t *testing.T) {
	for _, tt := range []struct {
		name, basePath, method, path string
		callerDeadline, wantDeadline bool
	}{
		{name: "root", method: "POST", path: "/topics/topic_a/regenerate-title"},
		{name: "runtime prefix", basePath: "/runtime", method: "POST", path: "/topics/topic_a/regenerate-title"},
		{name: "console prefix", basePath: "/console/runtime", method: "POST", path: "/topics/topic_a/regenerate-title"},
		{name: "query", basePath: "/runtime", method: "POST", path: "/topics/topic_a/regenerate-title?source=console"},
		{name: "caller deadline", basePath: "/runtime", method: "POST", path: "/topics/topic_a/regenerate-title", callerDeadline: true, wantDeadline: true},
		{name: "stop", basePath: "/runtime", method: "POST", path: "/topics/topic_a/stop", wantDeadline: true},
		{name: "get", basePath: "/runtime", method: "GET", path: "/topics/topic_a/regenerate-title", wantDeadline: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newDaemonTaskClient("http://runtime.test"+tt.basePath, "token")
			client.client.Transport = topicTitleRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				_, hasDeadline := req.Context().Deadline()
				if hasDeadline != tt.wantDeadline {
					t.Errorf("proxy deadline for %s = %v, want %v", req.URL.Path, hasDeadline, tt.wantDeadline)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
			})
			ctx := context.Background()
			if tt.callerDeadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Minute)
				defer cancel()
			}
			if _, _, err := client.Proxy(ctx, tt.method, tt.path, nil, ""); err != nil {
				t.Fatal(err)
			}
			if client.client.Timeout != 20*time.Second {
				t.Fatal("changed shared client timeout")
			}
		})
	}
}

func TestNewDaemonTaskClientDownloadTransportHasResponseHeaderTimeout(t *testing.T) {
	client := newDaemonTaskClient("http://127.0.0.1", "token")
	if client.downloadClient == nil {
		t.Fatal("download client is nil")
	}
	transport, ok := client.downloadClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("download transport = %T, want *http.Transport", client.downloadClient.Transport)
	}
	if transport.ResponseHeaderTimeout <= 0 {
		t.Fatalf("response header timeout = %v, want a positive duration", transport.ResponseHeaderTimeout)
	}
	if client.downloadClient.Timeout != 0 {
		t.Fatalf("download client total timeout = %v, want 0 for streaming bodies", client.downloadClient.Timeout)
	}
}
