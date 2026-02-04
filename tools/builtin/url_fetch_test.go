package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
		Method    string
		UserAgent string
		Accept    string
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
			Accept:    r.Header.Get("Accept"),
			Body:      string(b),
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
	if req.Body != "hello" {
		t.Fatalf("expected body %q, got %q", "hello", req.Body)
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
		"url":             "https://example.test/file.pdf",
		"download_path":   "jsonbill/out.pdf",
		"download_mkdirs": true,
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	if f == nil {
		return nil, errors.New("nil roundTripFunc")
	}
	return f(r)
}
