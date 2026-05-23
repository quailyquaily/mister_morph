package larkapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestTenantTokenClientCachesTokenUntilRefreshWindow(t *testing.T) {
	var hits int32
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != TenantAccessTokenPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, TenantAccessTokenPath)
		}
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
			t.Fatalf("content-type = %q, want application/json", got)
		}
		var req TenantAccessTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.AppID != "cli_test" || req.AppSecret != "secret_test" {
			t.Fatalf("request = %#v", req)
		}
		_ = json.NewEncoder(w).Encode(TenantAccessTokenResponse{
			Code:              0,
			Msg:               "ok",
			TenantAccessToken: "t-one",
			Expire:            7200,
		})
	}))

	now := time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC)
	client := NewTenantTokenClient(server.Client, server.URL, "cli_test", "secret_test")
	client.now = func() time.Time { return now }

	first, err := client.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token() error = %v", err)
	}
	second, err := client.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token() error = %v", err)
	}

	if first != "t-one" || second != "t-one" {
		t.Fatalf("tokens = %q, %q, want t-one", first, second)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestTenantTokenClientRefreshesWithinThirtyMinuteWindow(t *testing.T) {
	var hits int32
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&hits, 1)
		token := "t-one"
		if count > 1 {
			token = "t-two"
		}
		_ = json.NewEncoder(w).Encode(TenantAccessTokenResponse{
			Code:              0,
			Msg:               "ok",
			TenantAccessToken: token,
			Expire:            7200,
		})
	}))

	now := time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC)
	client := NewTenantTokenClient(server.Client, server.URL, "cli_test", "secret_test")
	client.now = func() time.Time { return now }

	first, err := client.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token() error = %v", err)
	}

	now = now.Add(5401 * time.Second)
	second, err := client.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token() error = %v", err)
	}

	if first != "t-one" {
		t.Fatalf("first token = %q, want t-one", first)
	}
	if second != "t-two" {
		t.Fatalf("second token = %q, want t-two", second)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
}

func TestTenantTokenClientReturnsAPIError(t *testing.T) {
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(TenantAccessTokenResponse{
			Code: 99991663,
			Msg:  "invalid app credential",
		})
	}))

	client := NewTenantTokenClient(server.Client, server.URL, "cli_test", "secret_test")
	_, err := client.Token(context.Background())
	if err == nil {
		t.Fatalf("Token() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "invalid app credential") {
		t.Fatalf("Token() error = %q, want invalid app credential", err.Error())
	}
}
