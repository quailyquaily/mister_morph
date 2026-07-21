package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
)

func TestURLFetchTool_DefaultGET(t *testing.T) {
	type got struct {
		Method    string
		UserAgent string
		Body      string
	}
	ch := make(chan got, 1)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Body != nil {
			defer r.Body.Close()
		}
		var b []byte
		if r.Body != nil {
			b, _ = io.ReadAll(r.Body)
		}
		ch <- got{
			Method:    r.Method,
			UserAgent: r.Header.Get("User-Agent"),
			Body:      string(b),
		}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.test/",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	req := <-ch
	if req.Method != http.MethodGet {
		t.Fatalf("expected method %q, got %q", http.MethodGet, req.Method)
	}
	if req.UserAgent != "test-agent" {
		t.Fatalf("expected user-agent %q, got %q", "test-agent", req.UserAgent)
	}
	if req.Body != "" {
		t.Fatalf("expected empty body, got %q", req.Body)
	}
}

func TestURLFetchTool_POSTHeadersBody(t *testing.T) {
	type got struct {
		Method      string
		UserAgent   string
		Accept      string
		ContentType string
		Body        string
	}
	ch := make(chan got, 1)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Body != nil {
			defer r.Body.Close()
		}
		var b []byte
		if r.Body != nil {
			b, _ = io.ReadAll(r.Body)
		}
		ch <- got{
			Method:      r.Method,
			UserAgent:   r.Header.Get("User-Agent"),
			Accept:      r.Header.Get("Accept"),
			ContentType: r.Header.Get("Content-Type"),
			Body:        string(b),
		}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "default-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":    "https://example.test/",
		"method": "POST",
		"headers": map[string]any{
			"User-Agent": "custom-agent",
			"Accept":     "application/json",
		},
		"body": "hello",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	req := <-ch
	if req.Method != http.MethodPost {
		t.Fatalf("expected method %q, got %q", http.MethodPost, req.Method)
	}
	if req.UserAgent != "custom-agent" {
		t.Fatalf("expected user-agent %q, got %q", "custom-agent", req.UserAgent)
	}
	if req.Accept != "application/json" {
		t.Fatalf("expected accept %q, got %q", "application/json", req.Accept)
	}
	if req.ContentType != "" {
		t.Fatalf("expected empty content-type when headers are provided without content-type, got %q", req.ContentType)
	}
	if req.Body != "hello" {
		t.Fatalf("expected body %q, got %q", "hello", req.Body)
	}
}

func TestURLFetchTool_PostObjectBodyInfersJSONContentTypeWithoutHeaders(t *testing.T) {
	type got struct {
		ContentType string
		Body        string
	}
	ch := make(chan got, 1)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Body != nil {
			defer r.Body.Close()
		}
		raw, _ := io.ReadAll(r.Body)
		ch <- got{
			ContentType: r.Header.Get("Content-Type"),
			Body:        string(raw),
		}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":    "https://example.test/",
		"method": "POST",
		"body": map[string]any{
			"model": "gpt-4o-mini-tts",
		},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	req := <-ch
	if req.ContentType != "application/json" {
		t.Fatalf("expected content-type %q, got %q", "application/json", req.ContentType)
	}
	if !strings.Contains(req.Body, "\"model\":\"gpt-4o-mini-tts\"") {
		t.Fatalf("expected json body, got %q", req.Body)
	}
}

func TestURLFetchTool_PostJSONStringBodyInfersJSONContentTypeWithoutHeaders(t *testing.T) {
	type got struct {
		ContentType string
	}
	ch := make(chan got, 1)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		ch <- got{ContentType: r.Header.Get("Content-Type")}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":    "https://example.test/",
		"method": "POST",
		"body":   "{\"model\":\"gpt-4o-mini-tts\"}",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	req := <-ch
	if req.ContentType != "application/json" {
		t.Fatalf("expected content-type %q, got %q", "application/json", req.ContentType)
	}
}

func TestURLFetchTool_PostPlainStringBodyInfersTextPlainWithoutHeaders(t *testing.T) {
	type got struct {
		ContentType string
	}
	ch := make(chan got, 1)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		ch <- got{ContentType: r.Header.Get("Content-Type")}
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":    "https://example.test/",
		"method": "POST",
		"body":   "hello world",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	req := <-ch
	if req.ContentType != "text/plain" {
		t.Fatalf("expected content-type %q, got %q", "text/plain", req.ContentType)
	}
}

func TestURLFetchTool_BodyWithDELETE_Unsupported(t *testing.T) {
	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":    "http://example.com",
		"method": "DELETE",
		"body":   "x",
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "curl") {
		t.Fatalf("expected error mentioning curl, got %v", err)
	}
}

func TestURLFetchTool_DownloadPathWritesRawBytes(t *testing.T) {
	cacheDir := t.TempDir()
	want := []byte{0x25, 0x50, 0x44, 0x46, 0x2d, 0x31, 0x2e, 0x33, 0x00, 0xff, 0x01}

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "application/pdf")
		return &http.Response{
			StatusCode: 200,
			Header:     h,
			Body:       io.NopCloser(bytes.NewReader(want)),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", cacheDir)
	tool.HTTPClient = &http.Client{Transport: rt}

	out, err := tool.Execute(context.Background(), map[string]any{
		"url":           "https://example.test/file.pdf",
		"download_path": "jsonbill/out.pdf",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	var resp map[string]any
	if json.Unmarshal([]byte(out), &resp) != nil {
		t.Fatalf("expected JSON output, got %q", out)
	}
	abs, _ := resp["abs_path"].(string)
	if abs == "" {
		t.Fatalf("expected abs_path in output, got %q", out)
	}

	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("saved bytes mismatch: got=%v want=%v", got, want)
	}

	if !strings.Contains(abs, filepath.Clean(cacheDir)) {
		t.Fatalf("expected abs_path under cacheDir, got %q (cacheDir=%q)", abs, cacheDir)
	}
}

func TestURLFetchTool_DownloadPathTruncationFails(t *testing.T) {
	cacheDir := t.TempDir()
	body := []byte("0123456789")

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 3, "test-agent", cacheDir)
	tool.HTTPClient = &http.Client{Transport: rt}
	tool.MaxBytesDownload = 3

	out, err := tool.Execute(context.Background(), map[string]any{
		"url":           "https://example.test/file.pdf",
		"download_path": "out.pdf",
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
	if _, statErr := os.Stat(filepath.Join(cacheDir, "out.pdf")); statErr == nil {
		t.Fatalf("expected file not to be written on truncation")
	}
}

func TestURLFetchTool_DebugLogsOutboundRequest(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":    "https://example.test/search?access_token=secret-token&q=test",
		"method": "POST",
		"headers": map[string]any{
			"Accept": "application/json",
		},
		"body": map[string]any{
			"api_key": "secret-api-key",
			"message": "hello",
		},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	logs := buf.String()
	if !strings.Contains(logs, "url_fetch_request_url") {
		t.Fatalf("expected debug url log, got %q", logs)
	}
	if !strings.Contains(logs, "url_fetch_request_method") || !strings.Contains(logs, "POST") {
		t.Fatalf("expected debug method log, got %q", logs)
	}
	if !strings.Contains(logs, "url_fetch_request_headers") {
		t.Fatalf("expected debug headers log, got %q", logs)
	}
	if strings.Contains(logs, "url_fetch_request_body") {
		t.Fatalf("expected request body log to be omitted in debug mode, got %q", logs)
	}
	if strings.Contains(logs, "url_fetch_response_raw_text") {
		t.Fatalf("expected response raw text log to be omitted in debug mode, got %q", logs)
	}
	if strings.Contains(logs, "secret-token") {
		t.Fatalf("expected url token to be redacted in logs, got %q", logs)
	}
	if strings.Contains(logs, "secret-api-key") {
		t.Fatalf("expected body api_key to be redacted in logs, got %q", logs)
	}
}

func TestURLFetchTool_InfoLevelSkipsOutboundDebugLogs(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.test/",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}

	logs := buf.String()
	if strings.Contains(logs, "url_fetch_request_url") ||
		strings.Contains(logs, "url_fetch_request_method") ||
		strings.Contains(logs, "url_fetch_request_headers") ||
		strings.Contains(logs, "url_fetch_request_body") ||
		strings.Contains(logs, "url_fetch_response_raw_text") {
		t.Fatalf("expected no debug outbound logs at info level, got %q", logs)
	}
}

func TestURLFetchTool_GuardRedirectPolicy(t *testing.T) {
	tests := []struct {
		name            string
		followRedirects bool
		location        string
		wantCalls       int
		wantErr         string
		wantBody        string
	}{
		{
			name:      "redirects disabled",
			location:  "/allowed/next",
			wantCalls: 1,
			wantErr:   "non-2xx status: 302",
		},
		{
			name:            "allowed same-origin redirect",
			followRedirects: true,
			location:        "/allowed/next",
			wantCalls:       2,
			wantBody:        "redirected",
		},
		{
			name:            "redirect outside prefix",
			followRedirects: true,
			location:        "/blocked",
			wantCalls:       1,
			wantErr:         "non_allowlisted_domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == "/allowed/start" {
					header := make(http.Header)
					header.Set("Location", tt.location)
					return &http.Response{
						StatusCode: http.StatusFound,
						Header:     header,
						Body:       io.NopCloser(strings.NewReader("redirect")),
						Request:    r,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("redirected")),
					Request:    r,
				}, nil
			})

			tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
			tool.HTTPClient = &http.Client{Transport: rt}
			ctx := guard.WithNetworkPolicy(context.Background(), guard.NetworkPolicy{
				AllowedURLPrefixes: []string{"https://example.test/allowed/"},
				FollowRedirects:    tt.followRedirects,
				AllowProxy:         true,
			})
			out, err := tool.Execute(ctx, map[string]any{
				"url": "https://example.test/allowed/start",
			})

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Execute() error = %v (out=%q)", err, out)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Execute() error = %v, want substring %q (out=%q)", err, tt.wantErr, out)
			}
			if calls != tt.wantCalls {
				t.Fatalf("RoundTrip calls = %d, want %d", calls, tt.wantCalls)
			}
			if tt.wantBody != "" && !strings.Contains(out, tt.wantBody) {
				t.Fatalf("Execute() output = %q, want body %q", out, tt.wantBody)
			}
		})
	}
}

func TestDisableHTTPProxy(t *testing.T) {
	proxyURL := &url.URL{Scheme: "http", Host: "proxy.example:8080"}
	original := http.DefaultTransport.(*http.Transport).Clone()
	original.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: original}

	disableHTTPProxy(client)

	withoutProxy, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport type = %T, want *http.Transport", client.Transport)
	}
	if withoutProxy == original {
		t.Fatal("disableHTTPProxy() mutated the shared transport instead of cloning it")
	}
	if withoutProxy.Proxy != nil {
		t.Fatal("disableHTTPProxy() left proxy resolution enabled")
	}
	if original.Proxy == nil {
		t.Fatal("disableHTTPProxy() changed the original transport")
	}

	defaultClient := &http.Client{}
	disableHTTPProxy(defaultClient)
	defaultTransport, ok := defaultClient.Transport.(*http.Transport)
	if !ok || defaultTransport.Proxy != nil {
		t.Fatalf("default client transport = %#v, want proxy-disabled *http.Transport", defaultClient.Transport)
	}
}

func TestNetworkPolicyDialContextRejectsPrivateDNSResolution(t *testing.T) {
	baseDialCalled := false
	dial := networkPolicyDialContext(
		func(context.Context, string, string) (net.Conn, error) {
			baseDialCalled = true
			return nil, errors.New("unexpected dial")
		},
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}, {IP: net.ParseIP("10.0.0.8")}}, nil
		},
		guard.NetworkPolicy{DenyPrivateIPs: true},
	)

	_, err := dial(context.Background(), "tcp", "example.test:443")
	if err == nil || !strings.Contains(err.Error(), "private_ip") {
		t.Fatalf("dial() error = %v, want private_ip", err)
	}
	if baseDialCalled {
		t.Fatal("base dial was called after a private DNS result")
	}
}

func TestURLFetchTool_ResponseBodyUsesGuardRedactor(t *testing.T) {
	const secret = "super-secret-value"
	body := `<html><body><p>MISTER_MORPH_API_KEY=` + secret + `</p>` +
		`<a href="/next?access_token=` + secret + `">next</a></body></html>`
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "text/html")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.test/",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}
	for _, leaked := range []string{"MISTER_MORPH_API_KEY", secret} {
		if strings.Contains(out, leaked) {
			t.Fatalf("Execute() output leaked %q: %q", leaked, out)
		}
	}
	if !strings.Contains(out, "[redacted_env]") {
		t.Fatalf("Execute() output = %q, want Guard env redaction marker", out)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	if f == nil {
		return nil, errors.New("nil roundTripFunc")
	}
	return f(r)
}
