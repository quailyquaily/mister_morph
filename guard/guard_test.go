package guard

import (
	"context"
	"testing"
)

func TestIsDeniedPrivateHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"", true},
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"93.184.216.34", false},  // example.com public IP
		{"8.8.8.8", false},        // Google DNS
		{"example.com", false},    // non-IP hostname → not denied at literal level
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			got := IsDeniedPrivateHost(tc.host)
			if got != tc.want {
				t.Fatalf("IsDeniedPrivateHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestResolveAndCheckHost_LiteralIPs(t *testing.T) {
	// No DNS lookup needed for literal IPs.
	cases := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{"loopback_v4", "127.0.0.1", true},
		{"loopback_v6", "::1", true},
		{"private_10", "10.0.0.1", true},
		{"private_172", "172.16.0.1", true},
		{"private_192", "192.168.1.1", true},
		{"link_local", "169.254.169.254", true},
		{"unspecified", "0.0.0.0", true},
		{"public_ip", "93.184.216.34", false},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ResolveAndCheckHost(tc.host, true, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ResolveAndCheckHost(%q) error=%v, wantErr=%v", tc.host, err, tc.wantErr)
			}
		})
	}
}

func TestResolveAndCheckHost_DNSResolvesToPrivate(t *testing.T) {
	fakeLookup := func(host string) ([]string, error) {
		// Simulate a hostname that resolves to a private IP.
		return []string{"127.0.0.1"}, nil
	}

	err := ResolveAndCheckHost("evil.example.com", true, fakeLookup)
	if err == nil {
		t.Fatal("expected error for hostname resolving to private IP, got nil")
	}
}

func TestResolveAndCheckHost_DNSResolvesToPublic(t *testing.T) {
	fakeLookup := func(host string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	}

	err := ResolveAndCheckHost("example.com", true, fakeLookup)
	if err != nil {
		t.Fatalf("expected nil error for public hostname, got: %v", err)
	}
}

func TestResolveAndCheckHost_ResolveDNSFalse(t *testing.T) {
	fakeLookup := func(host string) ([]string, error) {
		// This would return private IP, but ResolveDNS=false should skip it.
		return []string{"127.0.0.1"}, nil
	}

	// With ResolveDNS=false, a non-IP hostname passes (literal check only).
	err := ResolveAndCheckHost("evil.example.com", false, fakeLookup)
	if err != nil {
		t.Fatalf("expected nil error when ResolveDNS=false, got: %v", err)
	}
}

func TestNetworkPolicy_CheckHost(t *testing.T) {
	fakeLookup := func(host string) ([]string, error) {
		switch host {
		case "private.example.com":
			return []string{"10.0.0.1"}, nil
		case "public.example.com":
			return []string{"93.184.216.34"}, nil
		default:
			return []string{"93.184.216.34"}, nil
		}
	}

	pol := NetworkPolicy{
		DenyPrivateIPs: true,
		ResolveDNS:     true,
		LookupHost:     fakeLookup,
	}

	// Private hostname → blocked.
	if err := pol.CheckHost("private.example.com"); err == nil {
		t.Fatal("expected error for private-resolving hostname")
	}

	// Public hostname → allowed.
	if err := pol.CheckHost("public.example.com"); err != nil {
		t.Fatalf("expected nil for public hostname, got: %v", err)
	}

	// Literal private IP → blocked.
	if err := pol.CheckHost("127.0.0.1"); err == nil {
		t.Fatal("expected error for literal private IP")
	}

	// DenyPrivateIPs=false → everything allowed.
	polOpen := NetworkPolicy{DenyPrivateIPs: false, ResolveDNS: true, LookupHost: fakeLookup}
	if err := polOpen.CheckHost("127.0.0.1"); err != nil {
		t.Fatalf("expected nil when DenyPrivateIPs=false, got: %v", err)
	}
}

func TestGuard_SSRFDNSResolve(t *testing.T) {
	fakeLookup := func(host string) ([]string, error) {
		switch host {
		case "evil.test":
			return []string{"127.0.0.1"}, nil
		default:
			return []string{"93.184.216.34"}, nil
		}
	}

	g := New(Config{
		Enabled: true,
		Network: NetworkConfig{
			URLFetch: URLFetchNetworkPolicy{
				AllowedURLPrefixes: []string{"https://"},
				DenyPrivateIPs:     true,
				ResolveDNS:         true,
			},
		},
	}, nil, nil)
	g.SetLookupHost(fakeLookup)

	ctx := context.Background()
	meta := Meta{RunID: "test"}

	// Hostname resolving to private IP → deny.
	res, err := g.Evaluate(ctx, meta, Action{
		Type:       ActionToolCallPre,
		ToolName:   "url_fetch",
		ToolParams: map[string]any{"url": "https://evil.test/metadata"},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("expected deny for private-resolving host, got %s (reasons=%v)", res.Decision, res.Reasons)
	}
}

func TestGuard_SSRFPublicAllowed(t *testing.T) {
	fakeLookup := func(host string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	}

	g := New(Config{
		Enabled: true,
		Network: NetworkConfig{
			URLFetch: URLFetchNetworkPolicy{
				AllowedURLPrefixes: []string{"https://"},
				DenyPrivateIPs:     true,
				ResolveDNS:         true,
			},
		},
	}, nil, nil)
	g.SetLookupHost(fakeLookup)

	ctx := context.Background()
	meta := Meta{RunID: "test"}

	res, err := g.Evaluate(ctx, meta, Action{
		Type:       ActionToolCallPre,
		ToolName:   "url_fetch",
		ToolParams: map[string]any{"url": "https://public.example.com/api"},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("expected allow for public host, got %s (reasons=%v)", res.Decision, res.Reasons)
	}
}

func TestGuard_SSRFLiteralPrivateIP(t *testing.T) {
	g := New(Config{
		Enabled: true,
		Network: NetworkConfig{
			URLFetch: URLFetchNetworkPolicy{
				AllowedURLPrefixes: []string{"http://"},
				DenyPrivateIPs:     true,
				ResolveDNS:         true,
			},
		},
	}, nil, nil)

	ctx := context.Background()
	meta := Meta{RunID: "test"}

	res, err := g.Evaluate(ctx, meta, Action{
		Type:       ActionToolCallPre,
		ToolName:   "url_fetch",
		ToolParams: map[string]any{"url": "http://169.254.169.254/latest/meta-data/"},
	})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("expected deny for literal private IP, got %s", res.Decision)
	}
}

func TestURLAllowedByPrefixes(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		prefixes []string
		want     bool
	}{
		{"match", "https://api.example.com/v1/data", []string{"https://api.example.com/"}, true},
		{"no_match", "https://evil.com/exfil", []string{"https://api.example.com/"}, false},
		{"empty_prefixes", "https://anything.com/", nil, false},
		{"empty_url", "", []string{"https://api.example.com/"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := URLAllowedByPrefixes(tc.url, tc.prefixes)
			if got != tc.want {
				t.Fatalf("URLAllowedByPrefixes(%q, %v) = %v, want %v", tc.url, tc.prefixes, got, tc.want)
			}
		})
	}
}
