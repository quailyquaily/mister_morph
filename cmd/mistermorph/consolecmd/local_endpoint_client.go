package consolecmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

type inProcessRuntimeEndpointClient struct {
	view      func() inProcessRuntimeView
	canSubmit func() bool
}

type inProcessRuntimeView struct {
	handler   http.Handler
	authToken string
}

func newInProcessRuntimeEndpointClient(view func() inProcessRuntimeView, canSubmit func() bool) *inProcessRuntimeEndpointClient {
	return &inProcessRuntimeEndpointClient{
		view:      view,
		canSubmit: canSubmit,
	}
}

func (c *inProcessRuntimeEndpointClient) currentView() (inProcessRuntimeView, error) {
	if c == nil || c.view == nil {
		return inProcessRuntimeView{}, fmt.Errorf("daemon endpoint view getter is not configured")
	}
	view := c.view()
	if view.handler == nil {
		return inProcessRuntimeView{}, fmt.Errorf("daemon handler is not configured")
	}
	view.authToken = strings.TrimSpace(view.authToken)
	return view, nil
}

func (c *inProcessRuntimeEndpointClient) Health(ctx context.Context) (runtimeEndpointHealth, error) {
	status, _, raw, err := c.roundTrip(ctx, http.MethodGet, "/health", nil, false, "")
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

func (c *inProcessRuntimeEndpointClient) Proxy(ctx context.Context, method, endpointPath string, body []byte, contentType string) (int, []byte, error) {
	endpointPath = strings.TrimSpace(endpointPath)
	if endpointPath == "" {
		endpointPath = "/"
	}
	if !strings.HasPrefix(endpointPath, "/") {
		endpointPath = "/" + endpointPath
	}
	status, _, raw, err := c.roundTrip(ctx, method, endpointPath, body, true, contentType)
	return status, raw, err
}

func (c *inProcessRuntimeEndpointClient) Download(ctx context.Context, endpointPath string) (runtimeEndpointDownload, error) {
	view, err := c.currentView()
	if err != nil {
		return runtimeEndpointDownload{}, err
	}
	if view.authToken == "" {
		return runtimeEndpointDownload{}, fmt.Errorf("daemon server auth token is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	endpointPath = strings.TrimSpace(endpointPath)
	if endpointPath == "" {
		endpointPath = "/"
	}
	if !strings.HasPrefix(endpointPath, "/") {
		endpointPath = "/" + endpointPath
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointPath, http.NoBody)
	if err != nil {
		cancel()
		return runtimeEndpointDownload{}, err
	}
	req.Header.Set("Authorization", "Bearer "+view.authToken)

	reader, writer := io.Pipe()
	rec := newStreamingResponseWriter(writer)
	done := make(chan struct{})
	go func() {
		defer close(done)
		view.handler.ServeHTTP(rec, req)
		rec.finish()
	}()

	select {
	case <-rec.Ready():
	case <-ctx.Done():
		_ = reader.CloseWithError(ctx.Err())
		cancel()
		return runtimeEndpointDownload{}, ctx.Err()
	}

	return runtimeEndpointDownload{
		Status: rec.StatusCode(),
		Header: rec.HeaderClone(),
		Body: &streamingDownloadBody{
			reader: reader,
			cancel: cancel,
			done:   done,
		},
	}, nil
}

func (c *inProcessRuntimeEndpointClient) roundTrip(ctx context.Context, method, target string, body []byte, includeAuth bool, contentType string) (int, http.Header, []byte, error) {
	view, err := c.currentView()
	if err != nil {
		return 0, nil, nil, err
	}
	if includeAuth && view.authToken == "" {
		return 0, nil, nil, fmt.Errorf("daemon server auth token is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	bodyReader := io.Reader(http.NoBody)
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, strings.TrimSpace(method), target, bodyReader)
	if err != nil {
		return 0, nil, nil, err
	}
	if includeAuth {
		req.Header.Set("Authorization", "Bearer "+view.authToken)
	}
	if len(body) > 0 {
		if strings.TrimSpace(contentType) == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}

	rec := newBufferedResponseWriter()
	view.handler.ServeHTTP(rec, req)
	return rec.StatusCode(), rec.Header().Clone(), rec.Body(), nil
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

type streamingResponseWriter struct {
	header http.Header
	writer *io.PipeWriter
	ready  chan struct{}

	mu        sync.Mutex
	readyOnce sync.Once
	status    int
}

func newStreamingResponseWriter(writer *io.PipeWriter) *streamingResponseWriter {
	return &streamingResponseWriter{
		header: make(http.Header),
		writer: writer,
		ready:  make(chan struct{}),
	}
}

func (w *streamingResponseWriter) Header() http.Header {
	return w.header
}

func (w *streamingResponseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.writer.Write(p)
}

func (w *streamingResponseWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	if w.status == 0 {
		w.status = statusCode
	}
	w.mu.Unlock()
	w.readyOnce.Do(func() {
		close(w.ready)
	})
}

func (w *streamingResponseWriter) Flush() {
	w.WriteHeader(http.StatusOK)
}

func (w *streamingResponseWriter) Ready() <-chan struct{} {
	return w.ready
}

func (w *streamingResponseWriter) StatusCode() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *streamingResponseWriter) HeaderClone() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.header.Clone()
}

func (w *streamingResponseWriter) finish() {
	w.WriteHeader(http.StatusOK)
	_ = w.writer.Close()
}

type streamingDownloadBody struct {
	reader *io.PipeReader
	cancel context.CancelFunc
	done   <-chan struct{}

	closeOnce sync.Once
	closeErr  error
}

func (b *streamingDownloadBody) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *streamingDownloadBody) Close() error {
	b.closeOnce.Do(func() {
		if b.cancel != nil {
			b.cancel()
		}
		b.closeErr = b.reader.Close()
		if b.done != nil {
			select {
			case <-b.done:
			default:
			}
		}
	})
	return b.closeErr
}
