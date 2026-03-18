package consolecmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type inProcessRuntimeEndpointClient struct {
	handler   http.Handler
	authToken string
	canSubmit func() bool
}

func newInProcessRuntimeEndpointClient(handler http.Handler, authToken string, canSubmit func() bool) *inProcessRuntimeEndpointClient {
	return &inProcessRuntimeEndpointClient{
		handler:   handler,
		authToken: strings.TrimSpace(authToken),
		canSubmit: canSubmit,
	}
}

func (c *inProcessRuntimeEndpointClient) readyHandler() error {
	if c == nil || c.handler == nil {
		return fmt.Errorf("daemon handler is not configured")
	}
	return nil
}

func (c *inProcessRuntimeEndpointClient) ready() error {
	if err := c.readyHandler(); err != nil {
		return err
	}
	if strings.TrimSpace(c.authToken) == "" {
		return fmt.Errorf("daemon server auth token is not configured")
	}
	return nil
}

func (c *inProcessRuntimeEndpointClient) Health(ctx context.Context) (runtimeEndpointHealth, error) {
	status, raw, err := c.roundTrip(ctx, http.MethodGet, "/health", nil, false)
	if err != nil {
		return runtimeEndpointHealth{}, err
	}
	health, err := parseHealthResponse(status, raw)
	if err != nil {
		return runtimeEndpointHealth{}, err
	}
	if c != nil && c.canSubmit != nil {
		health.CanSubmit = c.canSubmit()
	}
	return health, nil
}

func (c *inProcessRuntimeEndpointClient) Proxy(ctx context.Context, method, endpointPath string, body []byte) (int, []byte, error) {
	if err := c.ready(); err != nil {
		return 0, nil, err
	}
	endpointPath = strings.TrimSpace(endpointPath)
	if endpointPath == "" {
		endpointPath = "/"
	}
	if !strings.HasPrefix(endpointPath, "/") {
		endpointPath = "/" + endpointPath
	}
	return c.roundTrip(ctx, method, endpointPath, body, true)
}

func (c *inProcessRuntimeEndpointClient) roundTrip(ctx context.Context, method, target string, body []byte, includeAuth bool) (int, []byte, error) {
	if err := c.readyHandler(); err != nil {
		return 0, nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, strings.TrimSpace(method), target, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	if includeAuth {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := newBufferedResponseWriter()
	c.handler.ServeHTTP(rec, req)
	return rec.StatusCode(), rec.Body(), nil
}

type bufferedResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{
		header: make(http.Header),
	}
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *bufferedResponseWriter) WriteHeader(statusCode int) {
	if w.status != 0 {
		return
	}
	w.status = statusCode
}

func (w *bufferedResponseWriter) StatusCode() int {
	if w == nil || w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *bufferedResponseWriter) Body() []byte {
	if w == nil {
		return nil
	}
	return append([]byte(nil), w.body.Bytes()...)
}
