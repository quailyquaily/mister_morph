package guard

import (
	"net"
	"net/url"
	"path"
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
	if !urlAllowedByPrefixes(parsed, p.AllowedURLPrefixes) {
		return "non_allowlisted_domain"
	}
	return ""
}

func urlAllowedByPrefixes(target *url.URL, prefixes []string) bool {
	if target == nil {
		return false
	}
	for _, rawPrefix := range prefixes {
		prefix, err := url.Parse(strings.TrimSpace(rawPrefix))
		if err != nil || !prefix.IsAbs() || prefix.User != nil || strings.TrimSpace(prefix.Hostname()) == "" {
			continue
		}
		if sameURLPolicyOrigin(target, prefix) && urlPolicyPathHasPrefix(target, prefix) && urlPolicyQueryHasPrefix(target, prefix) {
			return true
		}
	}
	return false
}

func sameURLPolicyOrigin(target, prefix *url.URL) bool {
	if target == nil || prefix == nil {
		return false
	}
	targetScheme := strings.ToLower(strings.TrimSpace(target.Scheme))
	prefixScheme := strings.ToLower(strings.TrimSpace(prefix.Scheme))
	if targetScheme != prefixScheme || (targetScheme != "http" && targetScheme != "https") {
		return false
	}
	targetHost := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(target.Hostname())), ".")
	prefixHost := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(prefix.Hostname())), ".")
	return targetHost != "" && targetHost == prefixHost && effectiveURLPort(target) == effectiveURLPort(prefix)
}

func effectiveURLPort(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	if port := strings.TrimSpace(parsed.Port()); port != "" {
		return port
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func urlPolicyPathHasPrefix(target, prefix *url.URL) bool {
	targetPath := normalizedURLPolicyPath(target)
	prefixPath := normalizedURLPolicyPath(prefix)
	return strings.HasPrefix(targetPath, prefixPath)
}

func normalizedURLPolicyPath(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	rawPath := parsed.Path
	if rawPath == "" {
		return "/"
	}
	trailingSlash := strings.HasSuffix(rawPath, "/")
	cleaned := path.Clean("/" + strings.TrimPrefix(rawPath, "/"))
	if trailingSlash && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned
}

func urlPolicyQueryHasPrefix(target, prefix *url.URL) bool {
	if prefix == nil || strings.TrimSpace(prefix.RawQuery) == "" {
		return true
	}
	return target != nil && strings.HasPrefix(target.RawQuery, prefix.RawQuery)
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
