package consolecmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/codexauth"
	"github.com/spf13/viper"
)

func TestCodexRefreshRouteRequiresConsoleSession(t *testing.T) {
	srv := &server{
		cfg: serveConfig{
			stateDir:         t.TempDir(),
			passwordOptional: true,
			password:         "configured",
		},
		sessions: newSessionStore(""),
	}
	recorder := httptest.NewRecorder()
	srv.handler().ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/api/auth/codex/refresh", nil),
	)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestSetCodexAsDefaultLLMPreservesProfilesAndClearsDefaultCredentials(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(
		"llm:\n"+
			"  inference_provider: openai\n"+
			"  provider: openai_resp\n"+
			"  endpoint: https://api.openai.com/v1\n"+
			"  api_key: default-secret\n"+
			"  model: gpt-5.4\n"+
			"  cloudflare:\n"+
			"    api_token: cloudflare-secret\n"+
			"    account_id: cloudflare-account\n"+
			"  bedrock:\n"+
			"    aws_key: aws-key\n"+
			"    aws_secret: aws-secret\n"+
			"    region: us-east-1\n"+
			"    model_arn: arn:aws:bedrock:us-east-1:123:model/test\n"+
			"  profiles:\n"+
			"    backup:\n"+
			"      inference_provider: anthropic\n"+
			"      provider: anthropic\n"+
			"      model: claude-sonnet-5\n"+
			"      api_key: profile-secret\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	previousConfig, hadConfig := viper.Get("config"), viper.IsSet("config")
	viper.Set("config", configPath)
	t.Cleanup(func() {
		if hadConfig {
			viper.Set("config", previousConfig)
		} else {
			viper.Set("config", nil)
		}
	})

	if err := (&server{}).setCodexAsDefaultLLM(context.Background()); err != nil {
		t.Fatalf("setCodexAsDefaultLLM() error = %v", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, want := range []string{
		"inference_provider: " + codexauth.ProviderName,
		"provider: " + codexauth.ProviderName,
		"model: " + codexauth.DefaultModel,
		"backup:",
		"model: claude-sonnet-5",
		"api_key: profile-secret",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("updated config missing %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{
		"default-secret",
		"https://api.openai.com/v1",
		"cloudflare-secret",
		"cloudflare-account",
		"aws-key",
		"aws-secret",
		"us-east-1",
		"arn:aws:bedrock",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("updated config retained %q:\n%s", notWant, out)
		}
	}
}
