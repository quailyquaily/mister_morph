package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
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

func TestApplyXAIDefaultLLMConfig(t *testing.T) {
	out, err := applyXAIDefaultLLMConfig([]byte(`
user_agent: test
llm:
  inference_provider: openai
  provider: openai_resp
  endpoint: https://api.openai.com/v1
  model: gpt-5.2
  api_key: ${OPENAI_API_KEY}
  cloudflare:
    account_id: acc-old
    api_token: token-old
  bedrock:
    region: us-east-1
  aws:
    key: legacy-key
    secret: legacy-secret
    session_token: legacy-session
    profile: legacy-profile
  profiles:
    backup:
      provider: anthropic
      model: claude-sonnet-5
`))
	if err != nil {
		t.Fatalf("applyXAIDefaultLLMConfig() error = %v", err)
	}
	got := string(out)
	for _, want := range []string{
		"user_agent: test",
		"inference_provider: " + xaiauth.ProviderName,
		"provider: " + xaiauth.ProviderName,
		"model: " + xaiauth.DefaultModel,
		"backup:",
		"provider: anthropic",
		"model: claude-sonnet-5",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("serialized config missing %q: %s", want, got)
		}
	}
	for _, notWant := range []string{
		"endpoint:",
		"api_key:",
		"cloudflare:",
		"account_id:",
		"api_token:",
		"bedrock:",
		"aws:",
		"legacy-key",
		"legacy-secret",
		"legacy-session",
		"legacy-profile",
	} {
		if strings.Contains(got, notWant) {
			t.Fatalf("serialized config should remove %q: %s", notWant, got)
		}
	}
}

func TestRunXAILoginStoresTokenWithoutChangingConfigByDefault(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	originalConfig := []byte("llm:\n  provider: anthropic\n  model: claude-sonnet-5\n")
	if err := os.WriteFile(configPath, originalConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	previousConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	previousStateDir, hadStateDir := viper.Get("file_state_dir"), viper.IsSet("file_state_dir")
	viper.Set("config", configPath)
	viper.Set("file_state_dir", stateDir)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", previousConfig)
		} else {
			viper.Set("config", nil)
		}
		if hadStateDir {
			viper.Set("file_state_dir", previousStateDir)
		} else {
			viper.Set("file_state_dir", nil)
		}
	})

	var polls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeXAIAuthJSON(w, map[string]any{
				"issuer":                        xaiauth.DefaultIssuer,
				"device_authorization_endpoint": xaiauth.DefaultIssuer + "/oauth2/device/code",
				"token_endpoint":                xaiauth.DefaultIssuer + "/oauth2/token",
				"revocation_endpoint":           xaiauth.DefaultIssuer + "/oauth2/revoke",
			})
		case "/oauth2/device/code":
			writeXAIAuthJSON(w, map[string]any{
				"device_code":               "device-secret",
				"user_code":                 "ABCD-EFGH",
				"verification_uri":          "https://accounts.x.ai/device",
				"verification_uri_complete": "https://accounts.x.ai/device?user_code=ABCD-EFGH",
				"expires_in":                600,
				"interval":                  0.001,
			})
		case "/oauth2/token":
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusBadRequest)
				writeXAIAuthJSON(w, map[string]any{"error": "slow_down"})
				return
			}
			writeXAIAuthJSON(w, map[string]any{
				"access_token":  "access-secret",
				"refresh_token": "refresh-secret",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	})
	cfg := xaiauth.OAuthConfig{
		Issuer:     xaiauth.DefaultIssuer,
		ClientID:   "mistermorph-test-client",
		Scope:      "openid offline_access api:access",
		HTTPClient: testhttp.NewClient(handler),
	}
	var waits []time.Duration
	var output bytes.Buffer
	if err := runXAILogin(context.Background(), xaiLoginOptions{
		wait: func(_ context.Context, interval time.Duration) error {
			waits = append(waits, interval)
			return nil
		},
	}, cfg, &output); err != nil {
		t.Fatalf("runXAILogin() error = %v", err)
	}
	token, ok, err := xaiauth.ReadToken(stateDir)
	if err != nil || !ok {
		t.Fatalf("xaiauth.ReadToken() = %+v, %t, %v", token, ok, err)
	}
	if token.AccessToken != "access-secret" || token.RefreshToken != "refresh-secret" {
		t.Fatalf("stored token = %+v", token)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, originalConfig) {
		t.Fatalf("config changed without --set-default:\n%s", after)
	}
	for _, secret := range []string{"device-secret", "access-secret", "refresh-secret"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("login output leaked %q: %s", secret, output.String())
		}
	}
	if got := polls.Load(); got != 2 {
		t.Fatalf("token polls = %d, want 2", got)
	}
	if len(waits) != 2 || waits[0] != time.Millisecond || waits[1] != 5*time.Second+time.Millisecond {
		t.Fatalf("poll waits = %v, want [1ms 5.001s]", waits)
	}
}

func TestRunXAILogoutDeletesTokenWhenRevocationFails(t *testing.T) {
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
			writeXAIAuthJSON(w, map[string]any{
				"issuer":                        xaiauth.DefaultIssuer,
				"device_authorization_endpoint": xaiauth.DefaultIssuer + "/oauth2/device/code",
				"token_endpoint":                xaiauth.DefaultIssuer + "/oauth2/token",
				"revocation_endpoint":           xaiauth.DefaultIssuer + "/oauth2/revoke",
			})
		case "/oauth2/revoke":
			w.WriteHeader(http.StatusInternalServerError)
			writeXAIAuthJSON(w, map[string]any{
				"error":             "refresh-secret",
				"error_description": "refresh-secret",
			})
		default:
			http.NotFound(w, r)
		}
	})
	cfg := xaiauth.OAuthConfig{
		Issuer:     xaiauth.DefaultIssuer,
		ClientID:   "mistermorph-test-client",
		Scope:      "openid offline_access api:access",
		HTTPClient: testhttp.NewClient(handler),
	}
	var output bytes.Buffer
	var warnings bytes.Buffer
	if err := runXAILogout(context.Background(), stateDir, cfg, &output, &warnings); err != nil {
		t.Fatalf("runXAILogout() error = %v", err)
	}
	if _, ok, err := xaiauth.ReadToken(stateDir); err != nil || ok {
		t.Fatalf("token after logout: ok=%t err=%v", ok, err)
	}
	if !strings.Contains(warnings.String(), "could not revoke") {
		t.Fatalf("logout warning = %q", warnings.String())
	}
	if strings.Contains(output.String()+warnings.String(), "refresh-secret") {
		t.Fatalf("logout output leaked token: stdout=%q stderr=%q", output.String(), warnings.String())
	}
}

func writeXAIAuthJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
