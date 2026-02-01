package builtin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestURLFetchTool_DefaultGET(t *testing.T) {
	type got struct {
		Method    string
		UserAgent string
		Body      string
	}
	ch := make(chan got, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		ch <- got{
			Method:    r.Method,
			UserAgent: r.Header.Get("User-Agent"),
			Body:      string(b),
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent")
	out, err := tool.Execute(context.Background(), map[string]any{
		"url": srv.URL,
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
		Method    string
		UserAgent string
		XTest     string
		Body      string
	}
	ch := make(chan got, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		ch <- got{
			Method:    r.Method,
			UserAgent: r.Header.Get("User-Agent"),
			XTest:     r.Header.Get("X-Test"),
			Body:      string(b),
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "default-agent")
	out, err := tool.Execute(context.Background(), map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"headers": map[string]any{
			"User-Agent": "custom-agent",
			"X-Test":     "1",
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
	if req.XTest != "1" {
		t.Fatalf("expected x-test %q, got %q", "1", req.XTest)
	}
	if req.Body != "hello" {
		t.Fatalf("expected body %q, got %q", "hello", req.Body)
	}
}

func TestURLFetchTool_BodyWithDELETE_Unsupported(t *testing.T) {
	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent")
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
