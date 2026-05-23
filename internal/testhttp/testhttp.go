package testhttp

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const BaseURL = "http://test.local"

var defaultTransportMu sync.Mutex

type Server struct {
	URL    string
	Client *http.Client
}

// NewServer returns a URL/client pair backed by handler without opening a socket.
func NewServer(handler http.Handler) Server {
	return Server{
		URL:    BaseURL,
		Client: NewClient(handler),
	}
}

func NewClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: NewTransport(handler)}
}

func NewTransport(handler http.Handler) http.RoundTripper {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		serverReq := req.Clone(req.Context())
		serverReq.RequestURI = ""
		if serverReq.Host == "" && serverReq.URL != nil {
			serverReq.Host = serverReq.URL.Host
		}
		handler.ServeHTTP(rec, serverReq)
		return rec.Result(), nil
	})
}

func WithDefaultTransport(t testing.TB, handler http.Handler) string {
	t.Helper()
	defaultTransportMu.Lock()
	previous := http.DefaultTransport
	http.DefaultTransport = NewTransport(handler)
	t.Cleanup(func() {
		http.DefaultTransport = previous
		defaultTransportMu.Unlock()
	})
	return BaseURL
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
