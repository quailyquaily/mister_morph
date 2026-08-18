package main

import "testing"

func TestDefaultEndpointForSetupProvider(t *testing.T) {
	cases := []struct {
		name     string
		choice   string
		endpoint string
	}{
		{name: "openai compatible", choice: setupProviderOpenAICompatible, endpoint: "https://api.openai.com"},
		{name: "gemini", choice: setupProviderGemini, endpoint: "https://generativelanguage.googleapis.com"},
		{name: "anthropic", choice: setupProviderAnthropic, endpoint: "https://api.anthropic.com"},
		{name: "cloudflare", choice: setupProviderCloudflare, endpoint: "https://api.cloudflare.com/client/v4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultEndpointForSetupProvider(tc.choice); got != tc.endpoint {
				t.Fatalf("defaultEndpointForSetupProvider(%q) = %q, want %q", tc.choice, got, tc.endpoint)
			}
		})
	}
}

func TestNormalizeConfigProviderForSetup(t *testing.T) {
	if got := normalizeConfigProviderForSetup(setupProviderOpenAICompatible); got != "openai" {
		t.Fatalf("normalizeConfigProviderForSetup() = %q, want openai", got)
	}
	if got := normalizeConfigProviderForSetup(setupProviderCloudflare); got != "cloudflare" {
		t.Fatalf("normalizeConfigProviderForSetup() = %q, want cloudflare", got)
	}
}

func TestLoadEmbeddedSoulPresets(t *testing.T) {
	presets, err := loadEmbeddedSoulPresets()
	if err != nil {
		t.Fatalf("loadEmbeddedSoulPresets() error = %v", err)
	}
	if len(presets) != 4 {
		t.Fatalf("loadEmbeddedSoulPresets() len = %d, want 4", len(presets))
	}
	if presets[0].ID != "research_scholar" {
		t.Fatalf("first preset id = %q, want research_scholar", presets[0].ID)
	}
	if presets[len(presets)-1].ID != "dog" {
		t.Fatalf("last preset id = %q, want dog", presets[len(presets)-1].ID)
	}
	if presets[0].CLITitle == "" || presets[0].CLIDescription == "" {
		t.Fatalf("preset metadata should include CLI title and description")
	}
}
