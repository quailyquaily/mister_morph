package codexauth

import "testing"

func TestUsesAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		apiKey   string
		want     bool
	}{
		{name: "OAuth by default", want: false},
		{name: "API key without custom endpoint", apiKey: "provider-key", want: false},
		{name: "custom endpoint without API key", endpoint: "https://codex.example.test/api", want: false},
		{name: "custom endpoint with API key", endpoint: "https://codex.example.test/api", apiKey: "provider-key", want: true},
		{name: "official endpoint with API key", endpoint: DefaultAPIBase, apiKey: "provider-key", want: false},
		{name: "official v1 endpoint with API key", endpoint: DefaultAPIBase + "/v1", apiKey: "provider-key", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UsesAPIKey(tt.endpoint, tt.apiKey); got != tt.want {
				t.Fatalf("UsesAPIKey(%q, %q) = %v, want %v", tt.endpoint, tt.apiKey, got, tt.want)
			}
		})
	}
}
