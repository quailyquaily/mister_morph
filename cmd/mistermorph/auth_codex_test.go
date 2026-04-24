package main

import (
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/codexauth"
)

func TestApplyCodexDefaultLLMConfig(t *testing.T) {
	out, err := applyCodexDefaultLLMConfig([]byte(`
user_agent: test
llm:
  provider: openai
  endpoint: https://api.openai.com
  model: gpt-5.2
  api_key: ${OPENAI_API_KEY}
  cloudflare:
    account_id: acc-old
    api_token: token-old
`))
	if err != nil {
		t.Fatalf("applyCodexDefaultLLMConfig() error = %v", err)
	}
	got := string(out)
	for _, want := range []string{
		"user_agent: test",
		"provider: " + codexauth.ProviderName,
		"model: " + codexauth.DefaultModel,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("serialized config missing %q: %s", want, got)
		}
	}
	for _, notWant := range []string{"endpoint:", "api_key:", "cloudflare:", "account_id:", "api_token:"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("serialized config should remove %q: %s", notWant, got)
		}
	}
}

func TestCodexLoginCurrentLLMConfigEmpty(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		runtimeCfg codexLoginRuntimeConfig
		want       bool
	}{
		{
			name: "blank file and blank runtime",
			want: true,
		},
		{
			name: "missing llm and runtime api key",
			runtimeCfg: codexLoginRuntimeConfig{
				APIKey: "sk-runtime",
			},
			want: false,
		},
		{
			name: "openai endpoint configured",
			data: "llm:\n  provider: openai\n  endpoint: https://api.openai.com\n",
			want: false,
		},
		{
			name: "openai api key placeholder configured",
			data: "llm:\n  provider: openai\n  api_key: ${OPENAI_API_KEY}\n",
			want: false,
		},
		{
			name: "openai empty credentials",
			data: "llm:\n  provider: openai\n  endpoint: \"\"\n  api_key: \"\"\n",
			want: true,
		},
		{
			name: "cloudflare nested token configured",
			data: "llm:\n  provider: cloudflare\n  cloudflare:\n    api_token: ${CLOUDFLARE_API_TOKEN}\n",
			want: false,
		},
		{
			name: "cloudflare empty credentials",
			data: "llm:\n  provider: cloudflare\n  cloudflare:\n    account_id: \"\"\n    api_token: \"\"\n",
			want: true,
		},
		{
			name: "cloudflare runtime account configured",
			data: "llm:\n  provider: cloudflare\n",
			runtimeCfg: codexLoginRuntimeConfig{
				Provider:            "cloudflare",
				CloudflareAccountID: "acc-runtime",
			},
			want: false,
		},
		{
			name: "non mapping llm is not auto empty",
			data: "llm: openai\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := codexLoginCurrentLLMConfigEmpty([]byte(tt.data), tt.runtimeCfg)
			if err != nil {
				t.Fatalf("codexLoginCurrentLLMConfigEmpty() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("codexLoginCurrentLLMConfigEmpty() = %t, want %t", got, tt.want)
			}
		})
	}
}
