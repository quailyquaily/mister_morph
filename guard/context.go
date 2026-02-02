package guard

import (
	"context"
	"fmt"
	"net"
	"strings"
)

type ctxKeyNetworkPolicy struct{}

type NetworkPolicy struct {
	AllowedURLPrefixes []string
	DenyPrivateIPs     bool
	ResolveDNS         bool
	FollowRedirects    bool
	AllowProxy         bool

	// LookupHost overrides net.LookupHost for testing. Nil uses the default resolver.
	LookupHost func(host string) ([]string, error)
}

// CheckHost checks whether a hostname or IP should be blocked by this policy.
// It checks literal private IPs and, when ResolveDNS is true, resolves hostnames
// and checks all returned IPs.
func (p NetworkPolicy) CheckHost(host string) error {
	if !p.DenyPrivateIPs {
		return nil
	}
	return ResolveAndCheckHost(host, p.ResolveDNS, p.LookupHost)
}

func WithNetworkPolicy(ctx context.Context, p NetworkPolicy) context.Context {
	return context.WithValue(ctx, ctxKeyNetworkPolicy{}, p)
}

func NetworkPolicyFromContext(ctx context.Context) (NetworkPolicy, bool) {
	if ctx == nil {
		return NetworkPolicy{}, false
	}
	v := ctx.Value(ctxKeyNetworkPolicy{})
	p, ok := v.(NetworkPolicy)
	return p, ok
}

// IsDeniedPrivateHost checks if a host string is a literal private, loopback,
// link-local, or unspecified address. Non-IP hostnames return false.
func IsDeniedPrivateHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return true
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	return false
}

// ResolveAndCheckHost checks a hostname for SSRF risk. It first checks literal
// private IPs, then (when resolveDNS is true) resolves the hostname via DNS and
// checks all returned IPs against loopback/private/link-local/unspecified ranges.
// If DNS resolution fails, the request is allowed through (it will fail at the HTTP layer).
func ResolveAndCheckHost(host string, resolveDNS bool, lookupHost func(string) ([]string, error)) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("empty hostname")
	}
	if IsDeniedPrivateHost(host) {
		return fmt.Errorf("request to private/loopback address is not allowed: %s", host)
	}
	if !resolveDNS {
		return nil
	}
	// Literal IPs already handled above.
	if net.ParseIP(host) != nil {
		return nil
	}
	if lookupHost == nil {
		lookupHost = net.LookupHost
	}
	ips, err := lookupHost(host)
	if err != nil {
		return nil
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("hostname %s resolves to private/loopback address %s", host, ipStr)
		}
	}
	return nil
}

// URLAllowedByPrefixes checks whether a URL matches any of the allowed prefixes.
func URLAllowedByPrefixes(raw string, prefixes []string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	for _, p := range prefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(raw, p) {
			return true
		}
	}
	return false
}

