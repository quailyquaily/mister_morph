package consolecmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
	"github.com/quailyquaily/mistermorph/internal/xaiauth"
	"github.com/spf13/viper"
)

func TestXAIAuthLoginHandlersExposeOnlyPublicDeviceFieldsAndStoreToken(t *testing.T) {
	var polls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeConsoleXAIAuthJSON(w, map[string]any{
				"issuer":                        xaiauth.DefaultIssuer,
				"device_authorization_endpoint": xaiauth.DefaultIssuer + "/oauth2/device/code",
				"token_endpoint":                xaiauth.DefaultIssuer + "/oauth2/token",
				"revocation_endpoint":           xaiauth.DefaultIssuer + "/oauth2/revoke",
			})
		case "/oauth2/device/code":
			writeConsoleXAIAuthJSON(w, map[string]any{
				"device_code":               "device-secret",
				"user_code":                 "ABCD-EFGH",
				"verification_uri":          "https://accounts.x.ai/device",
				"verification_uri_complete": "https://accounts.x.ai/device?user_code=ABCD-EFGH",
				"expires_in":                600,
				"interval":                  1,
			})
		case "/oauth2/token":
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusBadRequest)
				writeConsoleXAIAuthJSON(w, map[string]any{"error": "authorization_pending"})
				return
			}
			writeConsoleXAIAuthJSON(w, map[string]any{
				"access_token":  "access-secret",
				"refresh_token": "refresh-secret",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	})
	stateDir := t.TempDir()
	srv := &server{
		cfg:       serveConfig{stateDir: stateDir},
		xaiLogins: newXAILoginStore(),
		xaiOAuth: xaiauth.OAuthConfig{
			Issuer:     xaiauth.DefaultIssuer,
			ClientID:   "mistermorph-test-client",
			Scope:      "openid offline_access api:access",
			HTTPClient: testhttp.NewClient(handler),
		},
	}

	startRec := httptest.NewRecorder()
	srv.handleXAIAuthLoginStart(startRec, httptest.NewRequest(http.MethodPost, "/api/auth/xai/login/start", nil))
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status = %d (%s)", startRec.Code, startRec.Body.String())
	}
	startBody := startRec.Body.String()
	for _, secret := range []string{"device-secret", "mistermorph-test-client", "access-secret", "refresh-secret"} {
		if strings.Contains(startBody, secret) {
			t.Fatalf("start response leaked %q: %s", secret, startBody)
		}
	}
	var start struct {
		SessionID               string `json:"session_id"`
		VerificationURL         string `json:"verification_url"`
		VerificationURLComplete string `json:"verification_url_complete"`
		UserCode                string `json:"user_code"`
		IntervalSeconds         int    `json:"interval_seconds"`
	}
	if err := json.NewDecoder(strings.NewReader(startBody)).Decode(&start); err != nil {
		t.Fatal(err)
	}
	if start.SessionID == "" || start.UserCode != "ABCD-EFGH" ||
		start.VerificationURL != "https://accounts.x.ai/device" ||
		start.VerificationURLComplete == "" || start.IntervalSeconds != 1 {
		t.Fatalf("start response = %+v", start)
	}

	pollBody := []byte(`{"session_id":"` + start.SessionID + `","set_default":false}`)
	pendingRec := httptest.NewRecorder()
	srv.handleXAIAuthLoginPoll(
		pendingRec,
		httptest.NewRequest(http.MethodPost, "/api/auth/xai/login/poll", bytes.NewReader(pollBody)),
	)
	if pendingRec.Code != http.StatusOK || !strings.Contains(pendingRec.Body.String(), `"pending":true`) {
		t.Fatalf("pending response = %d %s", pendingRec.Code, pendingRec.Body.String())
	}

	completeRec := httptest.NewRecorder()
	srv.handleXAIAuthLoginPoll(
		completeRec,
		httptest.NewRequest(http.MethodPost, "/api/auth/xai/login/poll", bytes.NewReader(pollBody)),
	)
	if completeRec.Code != http.StatusOK || strings.Contains(completeRec.Body.String(), "access-secret") ||
		strings.Contains(completeRec.Body.String(), "refresh-secret") {
		t.Fatalf("complete response = %d %s", completeRec.Code, completeRec.Body.String())
	}
	status := xaiauth.ReadStatus(stateDir, time.Now().UTC())
	if !status.LoggedIn || !status.RefreshTokenPresent {
		t.Fatalf("stored status = %+v", status)
	}
	if _, ok := srv.xaiLogins.Get(start.SessionID); ok {
		t.Fatal("completed login session was not deleted")
	}
}

func TestXAIAuthLoginSessionExpires(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeConsoleXAIAuthJSON(w, map[string]any{
				"issuer":                        xaiauth.DefaultIssuer,
				"device_authorization_endpoint": xaiauth.DefaultIssuer + "/oauth2/device/code",
				"token_endpoint":                xaiauth.DefaultIssuer + "/oauth2/token",
			})
		case "/oauth2/device/code":
			writeConsoleXAIAuthJSON(w, map[string]any{
				"device_code":      "device-secret",
				"user_code":        "ABCD-EFGH",
				"verification_uri": "https://accounts.x.ai/device",
				"expires_in":       0.001,
				"interval":         1,
			})
		default:
			http.NotFound(w, r)
		}
	})
	srv := &server{
		cfg:       serveConfig{stateDir: t.TempDir()},
		xaiLogins: newXAILoginStore(),
		xaiOAuth: xaiauth.OAuthConfig{
			Issuer:     xaiauth.DefaultIssuer,
			ClientID:   "mistermorph-test-client",
			Scope:      "openid offline_access api:access",
			HTTPClient: testhttp.NewClient(handler),
		},
	}
	startRec := httptest.NewRecorder()
	srv.handleXAIAuthLoginStart(startRec, httptest.NewRequest(http.MethodPost, "/api/auth/xai/login/start", nil))
	var start struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(startRec.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)

	pollRec := httptest.NewRecorder()
	srv.handleXAIAuthLoginPoll(
		pollRec,
		httptest.NewRequest(
			http.MethodPost,
			"/api/auth/xai/login/poll",
			strings.NewReader(`{"session_id":"`+start.SessionID+`"}`),
		),
	)
	if pollRec.Code != http.StatusNotFound {
		t.Fatalf("expired poll status = %d, want 404 (%s)", pollRec.Code, pollRec.Body.String())
	}
}

func TestXAIAuthRoutesRequireConsoleSession(t *testing.T) {
	srv := &server{
		cfg: serveConfig{
			passwordOptional: true,
			password:         "configured",
		},
		sessions:  newSessionStore(""),
		xaiLogins: newXAILoginStore(),
	}
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/xai/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body.String())
	}
}

func TestXAIAuthLogoutDeletesLocalTokenWhenRevocationFails(t *testing.T) {
	stateDir := t.TempDir()
	if err := xaiauth.WriteToken(stateDir, xaiauth.Token{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeConsoleXAIAuthJSON(w, map[string]any{
				"issuer":                        xaiauth.DefaultIssuer,
				"device_authorization_endpoint": xaiauth.DefaultIssuer + "/oauth2/device/code",
				"token_endpoint":                xaiauth.DefaultIssuer + "/oauth2/token",
				"revocation_endpoint":           xaiauth.DefaultIssuer + "/oauth2/revoke",
			})
		case "/oauth2/revoke":
			w.WriteHeader(http.StatusInternalServerError)
			writeConsoleXAIAuthJSON(w, map[string]any{
				"error":             "refresh-secret",
				"error_description": "refresh-secret",
			})
		default:
			http.NotFound(w, r)
		}
	})
	srv := &server{
		cfg: serveConfig{stateDir: stateDir},
		xaiOAuth: xaiauth.OAuthConfig{
			ClientID:   "mistermorph-test-client",
			Scope:      "openid offline_access model.request",
			HTTPClient: testhttp.NewClient(handler),
		},
	}
	rec := httptest.NewRecorder()
	srv.handleXAIAuthLogout(rec, httptest.NewRequest(http.MethodPost, "/api/auth/xai/logout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "revocation_warning") {
		t.Fatalf("logout response has no revocation warning: %s", body)
	}
	for _, secret := range []string{"access-secret", "refresh-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("logout response leaked %q: %s", secret, body)
		}
	}
	if _, ok, err := xaiauth.ReadToken(stateDir); err != nil || ok {
		t.Fatalf("token after logout: ok=%t err=%v", ok, err)
	}
}

func TestSetXAIAsDefaultLLMPreservesProfilesAndClearsDefaultCredentials(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n"+
			"  inference_provider: openai\n"+
			"  provider: openai_resp\n"+
			"  endpoint: https://api.openai.com/v1\n"+
			"  api_key: default-secret\n"+
			"  model: gpt-5.4\n"+
			"  profiles:\n"+
			"    backup:\n"+
			"      inference_provider: anthropic\n"+
			"      provider: anthropic\n"+
			"      model: claude-sonnet-5\n"+
			"      api_key: profile-secret\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	prevConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", prevConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	if err := (&server{}).setXAIAsDefaultLLM(); err != nil {
		t.Fatalf("setXAIAsDefaultLLM() error = %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, want := range []string{
		"inference_provider: xai_oauth",
		"provider: xai_oauth",
		"model: grok-4.5",
		"backup:",
		"model: claude-sonnet-5",
		"api_key: profile-secret",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("updated config missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "default-secret") || strings.Contains(out, "https://api.openai.com/v1") {
		t.Fatalf("updated config retained default credentials:\n%s", out)
	}
}

func writeConsoleXAIAuthJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
