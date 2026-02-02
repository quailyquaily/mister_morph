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
	// URLPrefixes is the primary allowlist syntax: each entry is a URL prefix like:
	//   https://api.example.com/v1/resource
	//
	// The actual request must match at least one prefix by:
	// - scheme
	// - hostname
	// - effective port (explicit or default 80/443)
	// - path prefix (segment-safe)
	URLPrefixes []string `mapstructure:"url_prefixes"`

	// Methods is the allowed HTTP method set (GET/POST/PUT/DELETE).
	Methods []string `mapstructure:"methods"`

	FollowRedirects bool  `mapstructure:"follow_redirects"`
	AllowProxy      bool  `mapstructure:"allow_proxy"`
	DenyPrivateIPs  *bool `mapstructure:"deny_private_ips"`

	ParsedURLPrefixes []URLPrefixRule `mapstructure:"-"`
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

type URLPrefixRule struct {
	Scheme     string
	Host       string
	Port       int
	PathPrefix string
}

func (p *AuthProfile) Validate() error {
	if p == nil {
		return fmt.Errorf("nil profile")
	}
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("profile id is empty")
	}
	if strings.TrimSpace(p.Credential.Kind) == "" {
		return fmt.Errorf("auth_profiles.%s.credential.kind is required", p.ID)
	}
	if strings.TrimSpace(p.Credential.SecretRef) == "" {
		return fmt.Errorf("auth_profiles.%s.credential.secret_ref is required", p.ID)
	}

	if len(p.Allow.URLPrefixes) == 0 {
		return fmt.Errorf("auth_profiles.%s.allow.url_prefixes is required (fail-closed)", p.ID)
	}
	if len(p.Allow.Methods) == 0 {
		return fmt.Errorf("auth_profiles.%s.allow.methods is required (fail-closed)", p.ID)
	}

	rules, err := parseURLPrefixRules(p.Allow.URLPrefixes, p.ID)
	if err != nil {
		return err
	}
	p.Allow.ParsedURLPrefixes = rules

	for _, m := range p.Allow.Methods {
		m = strings.ToUpper(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		switch m {
		case "GET", "POST", "PUT", "DELETE":
		default:
			return fmt.Errorf("auth_profiles.%s.allow.methods contains unsupported method: %q", p.ID, m)
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
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return fmt.Errorf("url host is empty")
	}

	m := strings.ToUpper(strings.TrimSpace(method))
	if !stringInSliceFold(m, p.Allow.Methods) {
		return fmt.Errorf("method %q not allowed by auth_profile %q", m, p.ID)
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

	cleanPath := u.Path
	if strings.TrimSpace(cleanPath) == "" {
		cleanPath = "/"
	}
	cleanPath = path.Clean(cleanPath)
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	rules := p.Allow.ParsedURLPrefixes
	if len(rules) == 0 && len(p.Allow.URLPrefixes) > 0 {
		parsed, err := parseURLPrefixRules(p.Allow.URLPrefixes, p.ID)
		if err != nil {
			return err
		}
		rules = parsed
	}
	if len(rules) == 0 {
		return fmt.Errorf("auth_profile %q has no usable allow.url_prefixes rules", p.ID)
	}

	port := effectivePort(u)
	ok := false
	for _, r := range rules {
		if scheme != r.Scheme {
			continue
		}
		if host != r.Host {
			continue
		}
		if port != r.Port {
			continue
		}
		if pathPrefixMatch(cleanPath, r.PathPrefix) {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("url %q not allowed by auth_profile %q", u.String(), p.ID)
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

func parseURLPrefixRules(prefixes []string, profileID string) ([]URLPrefixRule, error) {
	var out []URLPrefixRule
	seen := make(map[string]bool, len(prefixes))
	for _, raw := range prefixes {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("auth_profiles.%s.allow.url_prefixes contains invalid url: %q", profileID, raw)
		}
		if u.User != nil {
			return nil, fmt.Errorf("auth_profiles.%s.allow.url_prefixes must not include userinfo: %q", profileID, raw)
		}
		if strings.TrimSpace(u.Fragment) != "" || strings.TrimSpace(u.RawQuery) != "" {
			return nil, fmt.Errorf("auth_profiles.%s.allow.url_prefixes must not include query/fragment: %q", profileID, raw)
		}
		scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
		switch scheme {
		case "http", "https":
		default:
			return nil, fmt.Errorf("auth_profiles.%s.allow.url_prefixes contains unsupported scheme: %q", profileID, raw)
		}
		host := strings.ToLower(strings.TrimSpace(u.Hostname()))
		if host == "" {
			return nil, fmt.Errorf("auth_profiles.%s.allow.url_prefixes contains empty host: %q", profileID, raw)
		}
		port := effectivePort(u)
		if port <= 0 || port > 65535 {
			return nil, fmt.Errorf("auth_profiles.%s.allow.url_prefixes contains invalid port in %q", profileID, raw)
		}
		pp := strings.TrimSpace(u.Path)
		if pp == "" {
			pp = "/"
		}
		pp = path.Clean(pp)
		if !strings.HasPrefix(pp, "/") {
			pp = "/" + pp
		}

		key := scheme + "|" + host + "|" + strconv.Itoa(port) + "|" + pp
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, URLPrefixRule{
			Scheme:     scheme,
			Host:       host,
			Port:       port,
			PathPrefix: pp,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("auth_profiles.%s.allow.url_prefixes is empty (fail-closed)", profileID)
	}
	return out, nil
}

func pathPrefixMatch(requestPath string, prefix string) bool {
	rp := strings.TrimSpace(requestPath)
	if rp == "" {
		rp = "/"
	}
	pp := strings.TrimSpace(prefix)
	if pp == "" {
		pp = "/"
	}
	if pp == "/" {
		return true
	}
	if rp == pp {
		return true
	}
	return strings.HasPrefix(rp, pp+"/")
}
