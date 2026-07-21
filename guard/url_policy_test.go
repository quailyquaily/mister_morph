package guard

import (
	"context"
	"slices"
	"testing"
)

func TestGuardURLFetchPrecheckValidatesCompleteURL(t *testing.T) {
	tests := []struct {
		name     string
		prefixes []string
		denyIP   bool
		rawURL   string
		decision Decision
		reason   string
	}{
		{
			name:     "allowed https url",
			prefixes: []string{"https://example.test/api/"},
			rawURL:   "https://example.test/api/items",
			decision: DecisionAllow,
		},
		{
			name:     "unsupported scheme",
			prefixes: []string{"file://"},
			rawURL:   "file:///etc/passwd",
			decision: DecisionDeny,
			reason:   "unsupported_url_scheme",
		},
		{
			name:     "missing host",
			prefixes: []string{"https://"},
			rawURL:   "https:///api/items",
			decision: DecisionDeny,
			reason:   "invalid_url",
		},
		{
			name:     "outside allowed prefix",
			prefixes: []string{"https://example.test/api/"},
			rawURL:   "https://example.test/admin",
			decision: DecisionDeny,
			reason:   "non_allowlisted_domain",
		},
		{
			name:     "lookalike host cannot match allowed prefix",
			prefixes: []string{"https://example.test"},
			rawURL:   "https://example.test.evil/",
			decision: DecisionDeny,
			reason:   "non_allowlisted_domain",
		},
		{
			name:     "path traversal cannot match allowed prefix",
			prefixes: []string{"https://example.test/api/"},
			rawURL:   "https://example.test/api/../admin",
			decision: DecisionDeny,
			reason:   "non_allowlisted_domain",
		},
		{
			name:     "localhost",
			prefixes: []string{"http://"},
			denyIP:   true,
			rawURL:   "http://localhost/status",
			decision: DecisionDeny,
			reason:   "private_ip",
		},
		{
			name:     "private ipv4",
			prefixes: []string{"http://"},
			denyIP:   true,
			rawURL:   "http://10.0.0.8/status",
			decision: DecisionDeny,
			reason:   "private_ip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New(Config{
				Enabled: true,
				Network: NetworkConfig{URLFetch: URLFetchNetworkPolicy{
					AllowedURLPrefixes: tt.prefixes,
					DenyPrivateIPs:     tt.denyIP,
				}},
			}, nil, nil)

			got, err := g.Evaluate(context.Background(), Meta{}, Action{
				Type:     ActionToolCallPre,
				ToolName: "url_fetch",
				ToolParams: map[string]any{
					"url": tt.rawURL,
				},
			})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if got.Decision != tt.decision {
				t.Fatalf("Evaluate() decision = %q, want %q", got.Decision, tt.decision)
			}
			if tt.reason != "" && !slices.Contains(got.Reasons, tt.reason) {
				t.Fatalf("Evaluate() reasons = %v, want %q", got.Reasons, tt.reason)
			}
		})
	}
}
