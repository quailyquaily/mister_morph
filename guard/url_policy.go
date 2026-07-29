package guard

import (
	"net"
	"net/url"
	"strings"
)

// URLDenyReason returns an audit-safe reason when rawURL violates the policy.
// An empty reason means the URL is allowed.
func (p NetworkPolicy) URLDenyReason(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.User != nil {
		return "invalid_url"
	}

	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https":
	default:
		return "unsupported_url_scheme"
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return "invalid_url"
	}

	if len(p.AllowedURLPrefixes) == 0 {
		return "url_fetch_not_allowlisted"
	}
	if p.DenyPrivateIPs && isDeniedPrivateHost(parsed.Hostname()) {
		return "private_ip"
	}
	if !urlAllowedByPrefixes(rawURL, p.AllowedURLPrefixes) {
		return "non_allowlisted_domain"
	}
	return ""
}

func urlAllowedByPrefixes(rawURL string, prefixes []string) bool {
	for _, rawPrefix := range prefixes {
		prefix := strings.TrimSpace(rawPrefix)
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(rawURL, prefix) {
			return true
		}
	}
	return false
}

func isDeniedPrivateHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// IPDenyReason applies the same private-address rule at connection time after
// DNS resolution. An empty reason means the address is allowed.
func (p NetworkPolicy) IPDenyReason(ip net.IP) string {
	if p.DenyPrivateIPs && (ip == nil || isDeniedPrivateHost(ip.String())) {
		return "private_ip"
	}
	return ""
}
