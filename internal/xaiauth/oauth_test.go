package xaiauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

const (
	testClientID = "mistermorph-test-client"
	testScope    = "openid offline_access model.request"
)

func TestDefaultOAuthConfigUsesSharedXAIClient(t *testing.T) {
	const expectedScope = "openid profile offline_access grok-cli:access api:access"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(w, validDiscovery())
		case "/oauth2/device/code":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm() error = %v", err)
			}
			if got := r.Form.Get("client_id"); got != DefaultClientID {
				t.Fatalf("client_id = %q, want shared client %q", got, DefaultClientID)
			}
			if got := r.Form.Get("scope"); got != expectedScope {
				t.Fatalf("scope = %q, want %q", got, expectedScope)
			}
			writeTestJSON(w, map[string]any{
				"device_code":      "device-secret",
				"user_code":        "ABCD-EFGH",
				"verification_uri": "https://accounts.x.ai/activate",
				"expires_in":       900,
				"interval":         5,
			})
		default:
			http.NotFound(w, r)
		}
	})

	if _, err := RequestDeviceCode(context.Background(), OAuthConfig{
		HTTPClient: testhttp.NewClient(handler),
	}); err != nil {
		t.Fatalf("RequestDeviceCode() error = %v", err)
	}
}

func TestDeviceCodeLoginFlow(t *testing.T) {
	now := time.Date(2026, 7, 29, 1, 2, 3, 0, time.UTC)
	var tokenPollCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(w, validDiscovery())
		case "/oauth2/device/code":
			if r.Method != http.MethodPost {
				t.Fatalf("device method = %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse device form: %v", err)
			}
			if r.Form.Get("client_id") != testClientID {
				t.Fatalf("client_id = %q", r.Form.Get("client_id"))
			}
			if got := r.Form.Get("scope"); got != testScope {
				t.Fatalf("scope = %q, want %q", got, testScope)
			}
			writeTestJSON(w, map[string]any{
				"device_code":               "device-secret",
				"user_code":                 "ABCD-EFGH",
				"verification_uri":          "https://accounts.x.ai/activate",
				"verification_uri_complete": "https://accounts.x.ai/activate?code=ABCD-EFGH",
				"expires_in":                900,
				"interval":                  3,
			})
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if r.Form.Get("grant_type") != deviceCodeGrantType {
				t.Fatalf("grant_type = %q", r.Form.Get("grant_type"))
			}
			if r.Form.Get("client_id") != testClientID || r.Form.Get("device_code") != "device-secret" {
				t.Fatalf("token form = %v", r.Form)
			}
			if tokenPollCount.Add(1) == 1 {
				w.WriteHeader(http.StatusBadRequest)
				writeTestJSON(w, map[string]any{"error": "authorization_pending"})
				return
			}
			writeTestJSON(w, map[string]any{
				"access_token":  "access-secret",
				"refresh_token": "refresh-secret",
				"id_token":      "discard-me",
				"token_type":    "Bearer",
				"scope":         testScope,
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	})

	cfg := testOAuthConfig(handler, now)
	code, err := RequestDeviceCode(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RequestDeviceCode() error = %v", err)
	}
	if code.VerificationURL != "https://accounts.x.ai/activate" ||
		code.VerificationURLComplete != "https://accounts.x.ai/activate?code=ABCD-EFGH" ||
		code.UserCode != "ABCD-EFGH" ||
		code.Interval != 3*time.Second ||
		!code.ExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("device code = %+v", code)
	}

	_, err = PollDeviceCode(context.Background(), cfg, code)
	if !errors.Is(err, ErrAuthorizationPending) {
		t.Fatalf("first PollDeviceCode() error = %v, want pending", err)
	}

	token, err := PollDeviceCode(context.Background(), cfg, code)
	if err != nil {
		t.Fatalf("second PollDeviceCode() error = %v", err)
	}
	if token.AccessToken != "access-secret" || token.RefreshToken != "refresh-secret" {
		t.Fatalf("token = %+v", token)
	}
	if token.TokenType != "Bearer" || token.Scope != testScope {
		t.Fatalf("token metadata = %+v", token)
	}
	if !token.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("expires_at = %s", token.ExpiresAt)
	}
	raw, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "discard-me") || strings.Contains(string(raw), "id_token") {
		t.Fatalf("ID token was retained: %s", raw)
	}
}

func TestRequestDeviceCodeRejectsUntrustedDiscoveryEndpoint(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discovery := validDiscovery()
		discovery["token_endpoint"] = "https://attacker.example/oauth2/token"
		writeTestJSON(w, discovery)
	})

	_, err := RequestDeviceCode(context.Background(), testOAuthConfig(handler, time.Now().UTC()))
	if err == nil || !strings.Contains(err.Error(), "token_endpoint") {
		t.Fatalf("RequestDeviceCode() error = %v, want token endpoint validation error", err)
	}
}

func TestRequestDeviceCodeRejectsNonExactDiscoveryIssuer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discovery := validDiscovery()
		discovery["issuer"] = DefaultIssuer + "/"
		writeTestJSON(w, discovery)
	})

	_, err := RequestDeviceCode(context.Background(), testOAuthConfig(handler, time.Now().UTC()))
	if err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("RequestDeviceCode() error = %v, want exact issuer validation error", err)
	}
}

func TestRequestDeviceCodeDoesNotFollowDiscoveryRedirect(t *testing.T) {
	var requests atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "https://attacker.example/openid-configuration", http.StatusFound)
	})

	_, err := RequestDeviceCode(context.Background(), testOAuthConfig(handler, time.Now().UTC()))
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("RequestDeviceCode() error = %v, want redirect rejection", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestRequestDeviceCodeRequiresExpiresIn(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(w, validDiscovery())
		case "/oauth2/device/code":
			writeTestJSON(w, map[string]any{
				"device_code":      "device-secret",
				"user_code":        "ABCD-EFGH",
				"verification_uri": "https://accounts.x.ai/activate",
			})
		default:
			http.NotFound(w, r)
		}
	})

	_, err := RequestDeviceCode(context.Background(), testOAuthConfig(handler, time.Now().UTC()))
	if err == nil || !strings.Contains(err.Error(), "expires_in") {
		t.Fatalf("RequestDeviceCode() error = %v, want expires_in validation error", err)
	}
}

func TestPollDeviceCodeReportsSlowDown(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(w, validDiscovery())
		case "/oauth2/device/code":
			writeTestJSON(w, map[string]any{
				"device_code":      "device-secret",
				"user_code":        "ABCD-EFGH",
				"verification_uri": "https://accounts.x.ai/activate",
				"expires_in":       900,
				"interval":         3,
			})
		case "/oauth2/token":
			w.WriteHeader(http.StatusBadRequest)
			writeTestJSON(w, map[string]any{"error": "slow_down"})
		default:
			http.NotFound(w, r)
		}
	})
	cfg := testOAuthConfig(handler, time.Now().UTC())
	code, err := RequestDeviceCode(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = PollDeviceCode(context.Background(), cfg, code)
	if !errors.Is(err, ErrSlowDown) {
		t.Fatalf("PollDeviceCode() error = %v, want ErrSlowDown", err)
	}
}

func TestResolveTokenRefreshesOnceAcrossConcurrentCallers(t *testing.T) {
	now := time.Date(2026, 7, 29, 2, 0, 0, 0, time.UTC)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var refreshCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(w, validDiscovery())
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "refresh_token" ||
				r.Form.Get("refresh_token") != "refresh-old" {
				t.Fatalf("refresh form = %v", r.Form)
			}
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
				"access_token":  "access-new",
				"refresh_token": "refresh-new",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	})

	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		TokenType:    "Bearer",
		ExpiresAt:    now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := testOAuthConfig(handler, now)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errCh := make(chan error, 2)
	go func() {
		_, err := ResolveToken(ctx, stateDir, cfg)
		errCh <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatalf("refresh did not start: %v", ctx.Err())
	}
	go func() {
		_, err := ResolveToken(ctx, stateDir, cfg)
		errCh <- err
	}()
	close(release)

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("ResolveToken() error = %v", err)
		}
	}
	if got := refreshCount.Load(); got != 1 {
		t.Fatalf("refresh count = %d, want 1", got)
	}
	stored, ok, err := ReadToken(stateDir)
	if err != nil || !ok {
		t.Fatalf("ReadToken() = %+v, %t, %v", stored, ok, err)
	}
	if stored.AccessToken != "access-new" || stored.RefreshToken != "refresh-new" {
		t.Fatalf("stored token = %+v", stored)
	}
}

func TestRefreshRejectedTokenKeepsNewerStoredToken(t *testing.T) {
	now := time.Date(2026, 7, 29, 2, 30, 0, 0, time.UTC)
	var requestCount atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	})
	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  "access-new",
		RefreshToken: "refresh-new",
		TokenType:    "Bearer",
		ExpiresAt:    now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	token, err := RefreshRejectedToken(
		context.Background(),
		stateDir,
		testOAuthConfig(handler, now),
		"access-old",
	)
	if err != nil {
		t.Fatalf("RefreshRejectedToken() error = %v", err)
	}
	if token.AccessToken != "access-new" {
		t.Fatalf("token = %+v", token)
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("request count = %d, want 0", got)
	}
}

func TestRefreshErrorDoesNotExposeRefreshToken(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(w, validDiscovery())
		case "/oauth2/token":
			w.WriteHeader(http.StatusBadRequest)
			writeTestJSON(w, map[string]any{
				"error":             "invalid_grant_refresh-secret",
				"error_description": "refresh-secret was revoked",
			})
		default:
			http.NotFound(w, r)
		}
	})
	_, err := RefreshToken(context.Background(), testOAuthConfig(handler, time.Now().UTC()), "refresh-secret")
	if err == nil {
		t.Fatal("RefreshToken() expected error")
	}
	if strings.Contains(err.Error(), "refresh-secret") {
		t.Fatalf("RefreshToken() leaked secret: %v", err)
	}
}

func TestResolveTokenDeletesStoredTokenAfterInvalidGrant(t *testing.T) {
	now := time.Date(2026, 7, 29, 2, 45, 0, 0, time.UTC)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(w, validDiscovery())
		case "/oauth2/token":
			w.WriteHeader(http.StatusBadRequest)
			writeTestJSON(w, map[string]any{
				"error":             "invalid_grant",
				"error_description": "refresh-secret was revoked",
			})
		default:
			http.NotFound(w, r)
		}
	})
	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		TokenType:    "Bearer",
		ExpiresAt:    now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveToken(context.Background(), stateDir, testOAuthConfig(handler, now))
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("ResolveToken() error = %v, want ErrNotLoggedIn", err)
	}
	if strings.Contains(err.Error(), "refresh-secret") {
		t.Fatalf("ResolveToken() leaked secret: %v", err)
	}
	if _, ok, readErr := ReadToken(stateDir); readErr != nil || ok {
		t.Fatalf("ReadToken() after invalid_grant: ok=%t err=%v", ok, readErr)
	}
}

func TestRevokeTokenUsesRefreshToken(t *testing.T) {
	var revoked atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(w, validDiscovery())
		case "/oauth2/revoke":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("token") != "refresh-secret" ||
				r.Form.Get("token_type_hint") != "refresh_token" ||
				r.Form.Get("client_id") != testClientID {
				t.Fatalf("revoke form = %v", r.Form)
			}
			revoked.Store(true)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})
	err := RevokeToken(context.Background(), testOAuthConfig(handler, time.Now().UTC()), Token{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
	})
	if err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}
	if !revoked.Load() {
		t.Fatal("revoke endpoint was not called")
	}
}

func TestTokenStoreStatusAndPermissions(t *testing.T) {
	now := time.Date(2026, 7, 29, 3, 0, 0, 0, time.UTC)
	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		TokenType:    "Bearer",
		ExpiresAt:    now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(TokenPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token mode = %o, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(TokenPath(stateDir)))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("token directory mode = %o, want 0700", dirInfo.Mode().Perm())
	}
	if !strings.HasSuffix(filepath.ToSlash(TokenPath(stateDir)), "/auth/xai.json") {
		t.Fatalf("token path = %s", TokenPath(stateDir))
	}
	status := ReadStatus(stateDir, now)
	if !status.LoggedIn || !status.AccessTokenPresent || !status.RefreshTokenPresent ||
		status.AccessTokenExpired || !status.FileModeOK {
		t.Fatalf("status = %+v", status)
	}
}

func TestWriteTokenTightensExistingAuthDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are not available")
	}
	stateDir := t.TempDir()
	authDir := filepath.Dir(TokenPath(stateDir))
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteToken(stateDir, Token{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(authDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("token directory mode = %o, want 0700", info.Mode().Perm())
	}
}

func TestReadStatusRejectsWideAuthDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are not available")
	}
	now := time.Now().UTC()
	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		TokenType:    "Bearer",
		ExpiresAt:    now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	authDir := filepath.Dir(TokenPath(stateDir))
	if err := os.Chmod(authDir, 0o755); err != nil {
		t.Fatal(err)
	}

	status := ReadStatus(stateDir, now)
	if status.FileModeOK {
		t.Fatalf("status = %+v, want unsafe directory permissions", status)
	}
	if !strings.Contains(status.FileModeWarning, "directory") {
		t.Fatalf("warning = %q, want directory permission warning", status.FileModeWarning)
	}
}

func testOAuthConfig(handler http.Handler, now time.Time) OAuthConfig {
	return OAuthConfig{
		ClientID:   testClientID,
		Scope:      testScope,
		HTTPClient: testhttp.NewClient(handler),
		Now:        func() time.Time { return now },
	}
}

func validDiscovery() map[string]any {
	return map[string]any{
		"issuer":                        DefaultIssuer,
		"device_authorization_endpoint": DefaultIssuer + "/oauth2/device/code",
		"token_endpoint":                DefaultIssuer + "/oauth2/token",
		"revocation_endpoint":           DefaultIssuer + "/oauth2/revoke",
	}
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
