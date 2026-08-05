package daemonruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/agentsettings"
	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/spf13/viper"
)

type capturingCodexSettingsOwner struct {
	update agentsettings.AgentSettingsUpdate
}

func (o *capturingCodexSettingsOwner) View(context.Context) (agentsettings.AgentSettingsView, error) {
	return agentsettings.AgentSettingsView{}, nil
}

func (o *capturingCodexSettingsOwner) Update(_ context.Context, update agentsettings.AgentSettingsUpdate) (agentsettings.AgentSettingsView, error) {
	o.update = update
	return agentsettings.AgentSettingsView{}, nil
}

func (o *capturingCodexSettingsOwner) CurrentReader() agentsettings.Reader {
	return nil
}

func TestCodexAuthRoutesAreAvailableWithAgentSettings(t *testing.T) {
	stateDir := t.TempDir()
	if err := codexauth.WriteToken(stateDir, codexauth.Token{
		AccessToken:  "remote-access-token",
		RefreshToken: "remote-refresh-token",
	}); err != nil {
		t.Fatalf("WriteToken() error = %v", err)
	}
	reader := viper.New()
	reader.Set("file_state_dir", stateDir)

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken:            "token",
		AgentSettingsEnabled: true,
		AgentSettingsReader:  reader,
	})

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "status",
			method:     http.MethodGet,
			path:       "/auth/codex/status",
			wantStatus: http.StatusOK,
			wantBody:   `"logged_in":true`,
		},
		{
			name:       "login start route",
			method:     http.MethodGet,
			path:       "/auth/codex/login/start",
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   "method not allowed",
		},
		{
			name:       "login poll route",
			method:     http.MethodPost,
			path:       "/auth/codex/login/poll",
			body:       `{"session_id":"missing"}`,
			wantStatus: http.StatusNotFound,
			wantBody:   "codex login session not found or expired",
		},
		{
			name:       "refresh route",
			method:     http.MethodPost,
			path:       "/auth/codex/refresh",
			wantStatus: http.StatusOK,
			wantBody:   `"refreshed":false`,
		},
		{
			name:       "logout",
			method:     http.MethodPost,
			path:       "/auth/codex/logout",
			wantStatus: http.StatusOK,
			wantBody:   `"removed":true`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer token")
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
	if _, ok, err := codexauth.ReadToken(stateDir); err != nil || ok {
		t.Fatalf("remote token remains after logout: ok = %v, err = %v", ok, err)
	}
}

func TestCodexAuthRoutesRequireRuntimeAuthentication(t *testing.T) {
	reader := viper.New()
	reader.Set("file_state_dir", t.TempDir())

	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken:            "token",
		AgentSettingsEnabled: true,
		AgentSettingsReader:  reader,
	})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/codex/status", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestSetRuntimeCodexAsDefaultLLMClearsIncompatibleCredentials(t *testing.T) {
	owner := &capturingCodexSettingsOwner{}
	if err := setRuntimeCodexAsDefaultLLM(context.Background(), owner); err != nil {
		t.Fatalf("setRuntimeCodexAsDefaultLLM() error = %v", err)
	}

	fields := owner.update.LLM.LLMConfigFieldsUpdate
	assertStringUpdate := func(name string, got *string, want string) {
		t.Helper()
		if got == nil || *got != want {
			t.Fatalf("%s = %v, want %q", name, got, want)
		}
	}
	assertStringUpdate("inference_provider", fields.InferenceProvider, codexauth.ProviderName)
	assertStringUpdate("provider", fields.Provider, codexauth.ProviderName)
	assertStringUpdate("model", fields.Model, codexauth.DefaultModel)
	for name, value := range map[string]*string{
		"endpoint":              fields.Endpoint,
		"api_key":               fields.APIKey,
		"cloudflare_api_token":  fields.CloudflareAPIToken,
		"cloudflare_account_id": fields.CloudflareAccountID,
		"bedrock_aws_key":       fields.BedrockAWSKey,
		"bedrock_aws_secret":    fields.BedrockAWSSecret,
		"bedrock_region":        fields.BedrockRegion,
		"bedrock_model_arn":     fields.BedrockModelARN,
	} {
		assertStringUpdate(name, value, "")
	}
}
