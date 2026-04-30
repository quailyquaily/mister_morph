package codexauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeviceCodeLoginFlow(t *testing.T) {
	now := time.Date(2026, 4, 24, 1, 2, 3, 0, time.UTC)
	accessToken := testJWT(t, map[string]any{
		"exp": now.Add(time.Hour).Unix(),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acc_123",
			"chatgpt_plan_type":  "plus",
		},
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/deviceauth/usercode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode usercode body: %v", err)
		}
		if body["client_id"] != DefaultClientID {
			t.Fatalf("client_id = %q", body["client_id"])
		}
		writeTestJSON(w, map[string]any{
			"device_auth_id": "dev_123",
			"user_code":      "ABCD-EFGH",
			"interval":       "1",
		})
	})
	mux.HandleFunc("/api/accounts/deviceauth/token", func(w http.ResponseWriter, r *http.Request) {
		var body tokenPollRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode poll body: %v", err)
		}
		if body.DeviceAuthID != "dev_123" || body.UserCode != "ABCD-EFGH" {
			t.Fatalf("poll body = %+v", body)
		}
		writeTestJSON(w, map[string]any{
			"authorization_code": "code_123",
			"code_verifier":      "verifier_123",
		})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/x-www-form-urlencoded") {
			t.Fatalf("content-type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "code_123" {
			t.Fatalf("form = %v", r.Form)
		}
		writeTestJSON(w, map[string]any{
			"access_token":  accessToken,
			"refresh_token": "refresh_123",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := OAuthConfig{Issuer: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }}
	deviceCode, err := RequestDeviceCode(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RequestDeviceCode() error = %v", err)
	}
	if deviceCode.VerificationURL != server.URL+"/codex/device" || deviceCode.UserCode != "ABCD-EFGH" {
		t.Fatalf("device code = %+v", deviceCode)
	}

	token, err := CompleteDeviceCodeLogin(context.Background(), cfg, deviceCode)
	if err != nil {
		t.Fatalf("CompleteDeviceCodeLogin() error = %v", err)
	}
	if token.AccessToken != accessToken || token.RefreshToken != "refresh_123" {
		t.Fatalf("token = %+v", token)
	}
	if token.AccountID != "acc_123" || token.PlanType != "plus" {
		t.Fatalf("claims not extracted: %+v", token)
	}
}

func TestResolveTokenRefreshesExpiredAccessToken(t *testing.T) {
	now := time.Date(2026, 4, 24, 2, 0, 0, 0, time.UTC)
	refreshedAccess := testJWT(t, map[string]any{"exp": now.Add(2 * time.Hour).Unix()})
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/x-www-form-urlencoded") {
			t.Fatalf("content-type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh_old" {
			t.Fatalf("refresh form = %v", r.Form)
		}
		writeTestJSON(w, map[string]any{
			"access_token":  refreshedAccess,
			"refresh_token": "refresh_new",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  testJWT(t, map[string]any{"exp": now.Add(-time.Hour).Unix()}),
		RefreshToken: "refresh_old",
		AccountID:    "acc_old",
		CreatedAt:    now.Add(-24 * time.Hour),
		UpdatedAt:    now.Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}

	cfg := OAuthConfig{Issuer: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }}
	token, err := ResolveToken(context.Background(), stateDir, cfg)
	if err != nil {
		t.Fatalf("ResolveToken() error = %v", err)
	}
	if token.AccessToken != refreshedAccess || token.RefreshToken != "refresh_new" {
		t.Fatalf("token = %+v", token)
	}
	if token.AccountID != "acc_old" {
		t.Fatalf("account id = %q", token.AccountID)
	}
}

func TestResolveTokenSerializesConcurrentRefresh(t *testing.T) {
	now := time.Date(2026, 4, 24, 3, 0, 0, 0, time.UTC)
	refreshedAccess := testJWT(t, map[string]any{"exp": now.Add(2 * time.Hour).Unix()})
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var refreshCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		refreshCount.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeTestJSON(w, map[string]any{
			"access_token":  refreshedAccess,
			"refresh_token": "refresh_new",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  testJWT(t, map[string]any{"exp": now.Add(-time.Hour).Unix()}),
		RefreshToken: "refresh_old",
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := OAuthConfig{Issuer: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }}
	errCh := make(chan error, 2)
	go func() {
		_, err := ResolveToken(ctx, stateDir, cfg)
		errCh <- err
	}()

	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatalf("first refresh did not start: %v", ctx.Err())
	}

	go func() {
		_, err := ResolveToken(ctx, stateDir, cfg)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)

	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("ResolveToken() error = %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("ResolveToken() timed out: %v", ctx.Err())
		}
	}
	if got := refreshCount.Load(); got != 1 {
		t.Fatalf("refresh count = %d, want 1", got)
	}
}

func TestTokenStoreStatusAndPermissions(t *testing.T) {
	stateDir := t.TempDir()
	exp := time.Now().UTC().Add(time.Hour)
	if err := WriteToken(stateDir, Token{
		AccessToken:  testJWT(t, map[string]any{"exp": exp.Unix()}),
		RefreshToken: "refresh",
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}
	path := TokenPath(stateDir)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token mode = %o, want 0600", info.Mode().Perm())
	}

	status := ReadStatus(stateDir, time.Now().UTC())
	if !status.LoggedIn || !status.FileModeOK || status.AccessTokenExpired {
		t.Fatalf("status = %+v", status)
	}
	if !strings.HasSuffix(filepath.ToSlash(TokenPath(stateDir)), "/auth/codex.json") {
		t.Fatalf("token path = %s", TokenPath(stateDir))
	}
}

func TestTokenStoreStatusTreatsExpiredAccessOnlyTokenAsSignedOut(t *testing.T) {
	now := time.Date(2026, 4, 24, 4, 0, 0, 0, time.UTC)
	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken: testJWT(t, map[string]any{"exp": now.Add(-time.Minute).Unix()}),
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}

	status := ReadStatus(stateDir, now)
	if status.LoggedIn {
		t.Fatalf("status should be signed out for expired access-only token: %+v", status)
	}
	if !status.AccessTokenPresent || status.RefreshTokenPresent || !status.AccessTokenExpired {
		t.Fatalf("status token fields = %+v", status)
	}
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "none", "typ": "JWT"}
	headerData, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	claimsData, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(headerData) + "." +
		base64.RawURLEncoding.EncodeToString(claimsData) + ".sig"
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
