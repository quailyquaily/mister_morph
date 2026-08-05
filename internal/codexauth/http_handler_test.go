package codexauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestHTTPHandlerCompletesLoginOnOwningRuntime(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	accessToken := testJWT(t, map[string]any{"exp": now.Add(time.Hour).Unix()})
	authMux := http.NewServeMux()
	authMux.HandleFunc("/api/accounts/deviceauth/usercode", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, map[string]any{
			"device_auth_id": "device-1",
			"user_code":      "ABCD-EFGH",
			"interval":       1,
			"expires_in":     900,
		})
	})
	authMux.HandleFunc("/api/accounts/deviceauth/token", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, map[string]any{
			"authorization_code": "code-1",
			"code_verifier":      "verifier-1",
		})
	})
	authMux.HandleFunc("/oauth/token", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, map[string]any{
			"access_token":  accessToken,
			"refresh_token": "refresh-1",
		})
	})
	authServer := testhttp.NewServer(authMux)

	stateDir := t.TempDir()
	setDefaultCalls := 0
	handler := NewHTTPHandler(HTTPHandlerOptions{
		StateDir: stateDir,
		OAuth: OAuthConfig{
			Issuer:     authServer.URL,
			HTTPClient: authServer.Client,
			Now:        func() time.Time { return now },
		},
		SetDefault: func(context.Context) error {
			setDefaultCalls++
			return nil
		},
	})

	startRecorder := httptest.NewRecorder()
	handler.LoginStart(startRecorder, httptest.NewRequest(http.MethodPost, "/auth/codex/login/start", nil))
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("start status = %d, want %d (%s)", startRecorder.Code, http.StatusOK, startRecorder.Body.String())
	}
	var startPayload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(startRecorder.Body.Bytes(), &startPayload); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if strings.TrimSpace(startPayload.SessionID) == "" {
		t.Fatal("session_id is empty")
	}

	pollBody := `{"session_id":"` + startPayload.SessionID + `","set_default":true}`
	pollRecorder := httptest.NewRecorder()
	handler.LoginPoll(
		pollRecorder,
		httptest.NewRequest(http.MethodPost, "/auth/codex/login/poll", strings.NewReader(pollBody)),
	)
	if pollRecorder.Code != http.StatusOK {
		t.Fatalf("poll status = %d, want %d (%s)", pollRecorder.Code, http.StatusOK, pollRecorder.Body.String())
	}
	var pollPayload struct {
		Pending         bool `json:"pending"`
		SettingsUpdated bool `json:"settings_updated"`
	}
	if err := json.Unmarshal(pollRecorder.Body.Bytes(), &pollPayload); err != nil {
		t.Fatalf("decode poll response: %v", err)
	}
	if pollPayload.Pending || !pollPayload.SettingsUpdated {
		t.Fatalf("poll payload = %#v", pollPayload)
	}
	if setDefaultCalls != 1 {
		t.Fatalf("set default calls = %d, want 1", setDefaultCalls)
	}
	token, ok, err := ReadToken(stateDir)
	if err != nil {
		t.Fatalf("ReadToken() error = %v", err)
	}
	if !ok || token.AccessToken != accessToken || token.RefreshToken != "refresh-1" {
		t.Fatalf("stored token = %#v, ok = %v", token, ok)
	}
}

func TestHTTPHandlerRefreshesExpiredAccessToken(t *testing.T) {
	now := time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)
	refreshedAccessToken := testJWT(t, map[string]any{"exp": now.Add(time.Hour).Unix()})
	authMux := http.NewServeMux()
	authMux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-old" {
			t.Fatalf("refresh_token = %q, want refresh-old", got)
		}
		writeTestJSON(w, map[string]any{
			"access_token":  refreshedAccessToken,
			"refresh_token": "refresh-new",
		})
	})
	authServer := testhttp.NewServer(authMux)

	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  testJWT(t, map[string]any{"exp": now.Add(-time.Minute).Unix()}),
		RefreshToken: "refresh-old",
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}
	handler := NewHTTPHandler(HTTPHandlerOptions{
		StateDir: stateDir,
		OAuth: OAuthConfig{
			Issuer:     authServer.URL,
			HTTPClient: authServer.Client,
			Now:        func() time.Time { return now },
		},
	})

	recorder := httptest.NewRecorder()
	handler.Refresh(recorder, httptest.NewRequest(http.MethodPost, "/auth/codex/refresh", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload struct {
		Refreshed bool   `json:"refreshed"`
		Status    Status `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if !payload.Refreshed || !payload.Status.LoggedIn || payload.Status.AccessTokenExpired {
		t.Fatalf("refresh payload = %+v", payload)
	}
	token, ok, err := ReadToken(stateDir)
	if err != nil || !ok || token.AccessToken != refreshedAccessToken || token.RefreshToken != "refresh-new" {
		t.Fatalf("stored token after refresh = %+v, ok=%t, err=%v", token, ok, err)
	}
}

func TestHTTPHandlerRefreshReturnsSignedOutAfterRejectedRefreshToken(t *testing.T) {
	now := time.Date(2026, 8, 5, 14, 0, 0, 0, time.UTC)
	authMux := http.NewServeMux()
	authMux.HandleFunc("/oauth/token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeTestJSON(w, map[string]any{
			"message": "Your refresh token has already been used to generate a new access token. Please try signing in again.",
		})
	})
	authServer := testhttp.NewServer(authMux)

	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  testJWT(t, map[string]any{"exp": now.Add(-time.Minute).Unix()}),
		RefreshToken: "refresh-old",
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}
	handler := NewHTTPHandler(HTTPHandlerOptions{
		StateDir: stateDir,
		OAuth: OAuthConfig{
			Issuer:     authServer.URL,
			HTTPClient: authServer.Client,
			Now:        func() time.Time { return now },
		},
	})

	recorder := httptest.NewRecorder()
	handler.Refresh(recorder, httptest.NewRequest(http.MethodPost, "/auth/codex/refresh", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload struct {
		Refreshed     bool   `json:"refreshed"`
		RequiresLogin bool   `json:"requires_login"`
		Status        Status `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if payload.Refreshed || !payload.RequiresLogin || payload.Status.LoggedIn {
		t.Fatalf("refresh payload = %+v", payload)
	}
	if _, ok, err := ReadToken(stateDir); err != nil || ok {
		t.Fatalf("token remains after rejected refresh: ok=%t err=%v", ok, err)
	}
}

func TestHTTPHandlerRefreshDoesNotCallOAuthForUsableAccessToken(t *testing.T) {
	now := time.Date(2026, 8, 5, 15, 0, 0, 0, time.UTC)
	var oauthCalls atomic.Int32
	authServer := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		oauthCalls.Add(1)
		http.Error(w, "unexpected OAuth request", http.StatusInternalServerError)
	}))

	stateDir := t.TempDir()
	if err := WriteToken(stateDir, Token{
		AccessToken:  testJWT(t, map[string]any{"exp": now.Add(time.Hour).Unix()}),
		RefreshToken: "refresh-current",
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}
	handler := NewHTTPHandler(HTTPHandlerOptions{
		StateDir: stateDir,
		OAuth: OAuthConfig{
			Issuer:     authServer.URL,
			HTTPClient: authServer.Client,
			Now:        func() time.Time { return now },
		},
	})

	recorder := httptest.NewRecorder()
	handler.Refresh(recorder, httptest.NewRequest(http.MethodPost, "/auth/codex/refresh", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := oauthCalls.Load(); got != 0 {
		t.Fatalf("OAuth calls = %d, want 0", got)
	}
}
