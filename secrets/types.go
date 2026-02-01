package secrets

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	Enabled       bool              `mapstructure:"enabled"`
	AllowProfiles []string          `mapstructure:"allow_profiles"`
	Aliases       map[string]string `mapstructure:"aliases"`
}

type Credential struct {
	Kind      string `mapstructure:"kind"`
	SecretRef string `mapstructure:"secret_ref"`
}

type Allow struct {
	AllowedHosts        []string `mapstructure:"allowed_hosts"`
	AllowedSchemes      []string `mapstructure:"allowed_schemes"`
	AllowedMethods      []string `mapstructure:"allowed_methods"`
	AllowedPorts        []int    `mapstructure:"allowed_ports"`
	AllowedPathPrefixes []string `mapstructure:"allowed_path_prefixes"`

	FollowRedirects bool  `mapstructure:"follow_redirects"`
	AllowProxy      bool  `mapstructure:"allow_proxy"`
	DenyPrivateIPs  *bool `mapstructure:"deny_private_ips"`
}

type Inject struct {
	Location string `mapstructure:"location"`
	Name     string `mapstructure:"name"`
	Format   string `mapstructure:"format"`
}

type ToolBinding struct {
	Inject              Inject   `mapstructure:"inject"`
	AllowUserHeaders    bool     `mapstructure:"allow_user_headers"`
	UserHeaderAllowlist []string `mapstructure:"user_header_allowlist"`
}

type AuthProfile struct {
	ID         string                 `mapstructure:"-"`
	Credential Credential             `mapstructure:"credential"`
	Allow      Allow                  `mapstructure:"allow"`
	Bindings   map[string]ToolBinding `mapstructure:"bindings"`
}

func (p AuthProfile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("profile id is empty")
	}
	if strings.TrimSpace(p.Credential.Kind) == "" {
		return fmt.Errorf("auth_profiles.%s.credential.kind is required", p.ID)
	}
	if strings.TrimSpace(p.Credential.SecretRef) == "" {
		return fmt.Errorf("auth_profiles.%s.credential.secret_ref is required", p.ID)
	}

	if len(p.Allow.AllowedHosts) == 0 {
		return fmt.Errorf("auth_profiles.%s.allow.allowed_hosts is required (fail-closed)", p.ID)
	}
	if len(p.Allow.AllowedSchemes) == 0 {
		return fmt.Errorf("auth_profiles.%s.allow.allowed_schemes is required (fail-closed)", p.ID)
	}
	if len(p.Allow.AllowedMethods) == 0 {
		return fmt.Errorf("auth_profiles.%s.allow.allowed_methods is required (fail-closed)", p.ID)
	}

	for _, h := range p.Allow.AllowedHosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if strings.Contains(h, "*") {
			return fmt.Errorf("auth_profiles.%s.allow.allowed_hosts does not allow wildcards: %q", p.ID, h)
		}
		if strings.Contains(h, "://") || strings.Contains(h, "/") {
			return fmt.Errorf("auth_profiles.%s.allow.allowed_hosts must be hostnames only (no scheme/path): %q", p.ID, h)
		}
		if strings.Contains(h, ":") {
			return fmt.Errorf("auth_profiles.%s.allow.allowed_hosts must not include ports: %q", p.ID, h)
		}
	}

	for _, s := range p.Allow.AllowedSchemes {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		switch s {
		case "http", "https":
		default:
			return fmt.Errorf("auth_profiles.%s.allow.allowed_schemes contains unsupported scheme: %q", p.ID, s)
		}
	}

	for _, m := range p.Allow.AllowedMethods {
		m = strings.ToUpper(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		switch m {
		case "GET", "POST", "PUT", "DELETE":
		default:
			return fmt.Errorf("auth_profiles.%s.allow.allowed_methods contains unsupported method: %q", p.ID, m)
		}
	}

	for _, port := range p.Allow.AllowedPorts {
		if port <= 0 || port > 65535 {
			return fmt.Errorf("auth_profiles.%s.allow.allowed_ports contains invalid port: %d", p.ID, port)
		}
	}

	for _, prefix := range p.Allow.AllowedPathPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if !strings.HasPrefix(prefix, "/") {
			return fmt.Errorf("auth_profiles.%s.allow.allowed_path_prefixes entries must start with '/': %q", p.ID, prefix)
		}
	}

	b, ok := p.Bindings["url_fetch"]
	if !ok {
		return fmt.Errorf("auth_profiles.%s.bindings.url_fetch is required", p.ID)
	}
	if err := b.Validate("url_fetch"); err != nil {
		return fmt.Errorf("auth_profiles.%s.bindings.url_fetch: %w", p.ID, err)
	}

	// Optional: validate other bindings if provided.
	for toolName, binding := range p.Bindings {
		if strings.TrimSpace(toolName) == "" {
			continue
		}
		if err := binding.Validate(toolName); err != nil {
			return fmt.Errorf("auth_profiles.%s.bindings.%s: %w", p.ID, toolName, err)
		}
	}

	return nil
}

var headerNameRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func (b ToolBinding) Validate(toolName string) error {
	loc := strings.ToLower(strings.TrimSpace(b.Inject.Location))
	if loc == "" {
		return fmt.Errorf("inject.location is required")
	}
	switch loc {
	case "header":
		// MVP supported.
	default:
		return fmt.Errorf("unsupported inject.location for %s: %q", toolName, b.Inject.Location)
	}

	name := strings.TrimSpace(b.Inject.Name)
	if name == "" {
		return fmt.Errorf("inject.name is required")
	}
	if !headerNameRe.MatchString(name) {
		return fmt.Errorf("inject.name must match header token syntax (letters/digits/dash): %q", name)
	}

	format := strings.ToLower(strings.TrimSpace(b.Inject.Format))
	if format == "" {
		format = "raw"
	}
	switch format {
	case "raw", "bearer", "basic":
	default:
		return fmt.Errorf("unsupported inject.format: %q", b.Inject.Format)
	}

	return nil
}

func (p AuthProfile) DenyPrivateIPs() bool {
	if p.Allow.DenyPrivateIPs == nil {
		return true
	}
	return *p.Allow.DenyPrivateIPs
}

func (p AuthProfile) IsURLAllowed(u *url.URL, method string) error {
	if u == nil {
		return fmt.Errorf("nil url")
	}
	if u.User != nil {
		return fmt.Errorf("userinfo in URL is not allowed")
	}

	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if !stringInSliceFold(scheme, p.Allow.AllowedSchemes) {
		return fmt.Errorf("url scheme %q not allowed by auth_profile %q", scheme, p.ID)
	}

	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return fmt.Errorf("url host is empty")
	}
	if !stringInSliceFold(host, p.Allow.AllowedHosts) {
		return fmt.Errorf("url host %q not allowed by auth_profile %q", host, p.ID)
	}

	if p.DenyPrivateIPs() {
		if host == "localhost" {
			return fmt.Errorf("localhost is not allowed by auth_profile %q", p.ID)
		}
		if ip := net.ParseIP(host); ip != nil {
			if isPrivateIP(ip) {
				return fmt.Errorf("private ip %q is not allowed by auth_profile %q", host, p.ID)
			}
		}
	}

	m := strings.ToUpper(strings.TrimSpace(method))
	if !stringInSliceFold(m, p.Allow.AllowedMethods) {
		return fmt.Errorf("method %q not allowed by auth_profile %q", m, p.ID)
	}

	port := effectivePort(u)
	if len(p.Allow.AllowedPorts) == 0 {
		// Fail-closed: only default ports if not explicitly allowed.
		switch scheme {
		case "http":
			if port != 80 {
				return fmt.Errorf("port %d not allowed by auth_profile %q (allowed_ports is empty)", port, p.ID)
			}
		case "https":
			if port != 443 {
				return fmt.Errorf("port %d not allowed by auth_profile %q (allowed_ports is empty)", port, p.ID)
			}
		}
	} else {
		if !intInSlice(port, p.Allow.AllowedPorts) {
			return fmt.Errorf("port %d not allowed by auth_profile %q", port, p.ID)
		}
	}

	cleanPath := u.Path
	if strings.TrimSpace(cleanPath) == "" {
		cleanPath = "/"
	}
	cleanPath = path.Clean(cleanPath)
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	if len(p.Allow.AllowedPathPrefixes) > 0 {
		ok := false
		for _, prefix := range p.Allow.AllowedPathPrefixes {
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				continue
			}
			cp := path.Clean(prefix)
			if !strings.HasPrefix(cp, "/") {
				cp = "/" + cp
			}
			if strings.HasPrefix(cleanPath, cp) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("url path %q not allowed by auth_profile %q", cleanPath, p.ID)
		}
	}

	return nil
}

func effectivePort(u *url.URL) int {
	if u == nil {
		return 0
	}
	if p := strings.TrimSpace(u.Port()); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
	case "http":
		return 80
	case "https":
		return 443
	default:
		return 0
	}
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// Go 1.22: IsPrivate covers RFC1918 + fc00::/7.
	if ip.IsPrivate() {
		return true
	}
	return false
}

func stringInSliceFold(needle string, haystack []string) bool {
	n := strings.ToLower(strings.TrimSpace(needle))
	if n == "" {
		return false
	}
	for _, s := range haystack {
		if strings.ToLower(strings.TrimSpace(s)) == n {
			return true
		}
	}
	return false
}

func intInSlice(n int, haystack []int) bool {
	for _, x := range haystack {
		if x == n {
			return true
		}
	}
	return false
}
