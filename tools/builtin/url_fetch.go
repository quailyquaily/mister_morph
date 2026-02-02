package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/quailyquaily/mister_morph/guard"
	"github.com/quailyquaily/mister_morph/secrets"
)

type URLFetchAuth struct {
	Enabled       bool
	AllowProfiles map[string]bool
	Profiles      *secrets.ProfileStore
	Resolver      secrets.Resolver
}

type URLFetchTool struct {
	Enabled        bool
	Timeout        time.Duration
	MaxBytes       int64
	UserAgent      string
	HTTPClient     *http.Client
	AllowScheme    map[string]bool
	Auth         *URLFetchAuth
	FileCacheDir string
}

func NewURLFetchTool(enabled bool, timeout time.Duration, maxBytes int64, userAgent string, fileCacheDir string) *URLFetchTool {
	return NewURLFetchToolWithAuth(enabled, timeout, maxBytes, userAgent, fileCacheDir, nil)
}

func NewURLFetchToolWithAuth(enabled bool, timeout time.Duration, maxBytes int64, userAgent string, fileCacheDir string, auth *URLFetchAuth) *URLFetchTool {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "mister_morph/1.0 (+https://github.com/quailyquaily)"
	}
	fileCacheDir = strings.TrimSpace(fileCacheDir)
	if fileCacheDir == "" {
		fileCacheDir = "/var/cache/morph"
	}
	return &URLFetchTool{
		Enabled:   enabled,
		Timeout:   timeout,
		MaxBytes:  maxBytes,
		UserAgent: userAgent,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
		AllowScheme:  map[string]bool{"http": true, "https": true},
		Auth:         auth,
		FileCacheDir: fileCacheDir,
	}
}

func (t *URLFetchTool) Name() string { return "url_fetch" }

func (t *URLFetchTool) Description() string {
	return "Fetches an HTTP(S) URL (GET/POST/PUT/DELETE) and returns the response body (truncated)."
}

func (t *URLFetchTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to fetch (http/https).",
			},
			"method": map[string]any{
				"type":        "string",
				"description": "Optional HTTP method (GET/POST/PUT/DELETE). Defaults to GET.",
				"enum":        []string{"GET", "POST", "PUT", "DELETE"},
			},
			"auth_profile": map[string]any{
				"type":        "string",
				"description": "Optional auth profile id for credential injection (server-side). When set, secrets.enabled must be true and the profile must be allowlisted.",
			},
			"headers": map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"type": "string",
				},
				"description": "Optional HTTP headers to send. Values must be strings. Allowlist enforced (default: Accept, Content-Type, User-Agent, If-None-Match, If-Modified-Since, Range). Sensitive headers (Authorization/Cookie/Host/Proxy-*/X-Forwarded-* and any *api[-_]?key*/*token*) are rejected.",
			},
			"body": map[string]any{
				"type":        []string{"string", "object", "array", "number", "boolean", "null"},
				"description": "Optional request body (supported for POST and PUT). For binary responses, prefer download_path to save to a file instead of returning in the observation.",
			},
			"download_path": map[string]any{
				"type":        "string",
				"description": "Optional: if set, saves the raw response body to this path (under file_cache_dir) and returns JSON metadata instead of including the body in the output. Recommended for PDFs/binary.",
			},
			"download_mkdirs": map[string]any{
				"type":        "boolean",
				"description": "If true, creates parent directories for download_path under file_cache_dir.",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": "Optional timeout override in seconds.",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": "Optional max response bytes to read (truncates beyond this).",
			},
		},
		"required": []string{"url"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *URLFetchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if !t.Enabled {
		return "", fmt.Errorf("url_fetch tool is disabled (enable via config: tools.url_fetch.enabled=true)")
	}

	rawURL, _ := params["url"].(string)
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("missing required param: url")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if !t.AllowScheme[strings.ToLower(u.Scheme)] {
		return "", fmt.Errorf("unsupported url scheme: %s", u.Scheme)
	}

	authProfileID, _ := params["auth_profile"].(string)
	authProfileID = strings.TrimSpace(authProfileID)

	netPol, hasNetPol := guard.NetworkPolicyFromContext(ctx)
	if hasNetPol {
		if len(netPol.AllowedURLPrefixes) == 0 {
			return "", fmt.Errorf("url_fetch is blocked by guard (no allowed_url_prefixes configured)")
		}
		if !guard.URLAllowedByPrefixes(u.String(), netPol.AllowedURLPrefixes) {
			return "", fmt.Errorf("url is not allowed by guard")
		}
		if err := netPol.CheckHost(u.Hostname()); err != nil {
			return "", fmt.Errorf("host blocked by guard: %w", err)
		}
	}

	downloadPath, _ := params["download_path"].(string)
	downloadPath = strings.TrimSpace(downloadPath)
	downloadMkdirs := false
	if v, ok := params["download_mkdirs"].(bool); ok {
		downloadMkdirs = v
	}

	method := http.MethodGet
	if v, ok := params["method"]; ok {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("invalid param: method must be a string (for more complex requests, use the bash tool with curl)")
		}
		s = strings.ToUpper(strings.TrimSpace(s))
		if s != "" {
			method = s
		}
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
	default:
		return "", fmt.Errorf("unsupported method: %s (url_fetch supports GET, POST, PUT, DELETE; for other methods use the bash tool with curl)", method)
	}

	timeout := t.Timeout
	if v, ok := params["timeout_seconds"]; ok {
		if secs, ok := asFloat64(v); ok && secs > 0 {
			timeout = time.Duration(secs * float64(time.Second))
		}
	}

	maxBytes := t.MaxBytes
	if v, ok := params["max_bytes"]; ok {
		if n, ok := asInt64(v); ok && n > 0 {
			maxBytes = n
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var bodyReader io.Reader
	var bodyProvided bool
	var bodyIsNonStringJSON bool
	if v, ok := params["body"]; ok {
		bodyProvided = true
		if v != nil {
			switch x := v.(type) {
			case string:
				bodyReader = strings.NewReader(x)
			default:
				if authProfileID != "" {
					if offending := findExistingAbsPath(x); offending != "" {
						return "", fmt.Errorf("request body contains local file path %q; use read_file and send the file contents instead", offending)
					}
				}
				bodyIsNonStringJSON = true
				bodyBytes, err := json.Marshal(x)
				if err != nil {
					return "", fmt.Errorf("invalid param: body must be a string or JSON-serializable value (for more complex requests, use the bash tool with curl): %w", err)
				}
				bodyReader = bytes.NewReader(bodyBytes)
			}
		}
	}
	if bodyProvided && method != http.MethodPost && method != http.MethodPut {
		return "", fmt.Errorf("request body is only supported for POST/PUT in url_fetch (use the bash tool with curl for %s with a body)", method)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, u.String(), bodyReader)
	if err != nil {
		return "", err
	}

	var (
		profile          secrets.AuthProfile
		binding          secrets.ToolBinding
		injectHeaderName string
		injectHeaderVal  string
	)
	if authProfileID != "" {
		if pol, ok := secrets.SkillAuthProfilePolicyFromContext(ctx); ok && pol.Enforce {
			if pol.Allowed == nil || !pol.Allowed[authProfileID] {
				return "", fmt.Errorf("auth_profile %q is not declared by any loaded skill", authProfileID)
			}
		}
		if t.Auth == nil || !t.Auth.Enabled {
			return "", fmt.Errorf("auth_profile is not enabled (set secrets.enabled=true)")
		}
		if t.Auth.AllowProfiles == nil || !t.Auth.AllowProfiles[authProfileID] {
			return "", fmt.Errorf("auth_profile %q is not allowed (fail-closed)", authProfileID)
		}
		if t.Auth.Profiles == nil {
			return "", fmt.Errorf("auth_profile is enabled but profile store is not configured")
		}
		p, ok := t.Auth.Profiles.Get(authProfileID)
		if !ok {
			return "", fmt.Errorf("auth_profile not found: %q", authProfileID)
		}
		if err := p.Validate(); err != nil {
			return "", fmt.Errorf("invalid auth_profile %q: %w", authProfileID, err)
		}
		if err := p.IsURLAllowed(u, method); err != nil {
			return "", err
		}
		profile = p

		b, ok := p.Bindings[t.Name()]
		if !ok {
			return "", fmt.Errorf("auth_profile %q has no binding for tool %q", authProfileID, t.Name())
		}
		if err := b.Validate(t.Name()); err != nil {
			return "", fmt.Errorf("auth_profile %q binding invalid for tool %q: %w", authProfileID, t.Name(), err)
		}
		binding = b

		if t.Auth.Resolver == nil {
			return "", fmt.Errorf("auth_profile is enabled but secret resolver is not configured")
		}
		sec, err := t.Auth.Resolver.Resolve(reqCtx, p.Credential.SecretRef)
		if err != nil {
			return "", err
		}
		injectHeaderName = strings.TrimSpace(b.Inject.Name)
		injectHeaderVal, err = formatInjectedSecret(b.Inject.Format, sec)
		if err != nil {
			return "", err
		}
	}

	var hasUserAgent bool
	var hasContentType bool
	if hdrs, ok := params["headers"]; ok && hdrs != nil {
		m, ok := hdrs.(map[string]any)
		if !ok {
			return "", fmt.Errorf("invalid param: headers must be an object of string values (for more complex requests, use the bash tool with curl)")
		}
		if authProfileID != "" && !binding.AllowUserHeaders && len(m) > 0 {
			return "", fmt.Errorf("headers are not allowed when using auth_profile %q", authProfileID)
		}
		for k, v := range m {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			value, ok := v.(string)
			if !ok {
				return "", fmt.Errorf("invalid header %q: value must be a string (for more complex requests, use the bash tool with curl)", key)
			}
			value = strings.TrimSpace(value)
			if isDeniedUserHeader(key) {
				return "", fmt.Errorf("header %q is not allowed", key)
			}
			if !isAllowedUserHeader(key, authProfileID, binding) {
				return "", fmt.Errorf("header %q is not allowed (allowlist enforced)", key)
			}
			// Disallow setting Content-Length via headers; net/http will compute it.
			if strings.EqualFold(key, "content-length") {
				continue
			}
			if authProfileID != "" && strings.EqualFold(key, injectHeaderName) {
				return "", fmt.Errorf("header %q must not be provided when using auth_profile %q", injectHeaderName, authProfileID)
			}
			req.Header.Set(key, value)
			if strings.EqualFold(key, "user-agent") {
				hasUserAgent = true
			}
			if strings.EqualFold(key, "content-type") {
				hasContentType = true
			}
		}
	}

	if !hasUserAgent && strings.TrimSpace(t.UserAgent) != "" {
		req.Header.Set("User-Agent", t.UserAgent)
	}
	// If the caller passed a JSON-ish body (non-string), default Content-Type to application/json.
	if bodyIsNonStringJSON && !hasContentType {
		req.Header.Set("Content-Type", "application/json")
	}

	if authProfileID != "" {
		req.Header.Set(injectHeaderName, injectHeaderVal)
	}

	var client http.Client
	if t.HTTPClient != nil {
		client = *t.HTTPClient
	}
	client.Timeout = timeout
	if authProfileID != "" {
		origin := canonicalOrigin(u)
		maxRedirects := 3
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if !profile.Allow.FollowRedirects {
				return http.ErrUseLastResponse
			}
			if len(via) > maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if canonicalOrigin(req.URL) != origin {
				return fmt.Errorf("redirect to different origin is not allowed")
			}
			if err := profile.IsURLAllowed(req.URL, req.Method); err != nil {
				return err
			}
			req.Header.Set(injectHeaderName, injectHeaderVal)
			return nil
		}
		if !profile.Allow.AllowProxy {
			if client.Transport == nil {
				tr := cloneDefaultTransport()
				tr.Proxy = nil
				client.Transport = tr
			} else if tr, ok := client.Transport.(*http.Transport); ok && tr != nil {
				cp := tr.Clone()
				cp.Proxy = nil
				client.Transport = cp
			}
		}
	}
	if authProfileID == "" && hasNetPol {
		origin := canonicalOrigin(u)
		maxRedirects := 3
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if !netPol.FollowRedirects {
				return http.ErrUseLastResponse
			}
			if len(via) > maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if canonicalOrigin(req.URL) != origin {
				return fmt.Errorf("redirect to different origin is not allowed")
			}
			if err := netPol.CheckHost(req.URL.Hostname()); err != nil {
				return fmt.Errorf("redirect host blocked by guard: %w", err)
			}
			if !guard.URLAllowedByPrefixes(req.URL.String(), netPol.AllowedURLPrefixes) {
				return fmt.Errorf("redirect url is not allowed by guard")
			}
			return nil
		}
		if !netPol.AllowProxy {
			if client.Transport == nil {
				tr := cloneDefaultTransport()
				tr.Proxy = nil
				client.Transport = tr
			} else if tr, ok := client.Transport.(*http.Transport); ok && tr != nil {
				cp := tr.Clone()
				cp.Proxy = nil
				client.Transport = cp
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var truncated bool
	limitReader := io.LimitReader(resp.Body, maxBytes+1)
	body, err := io.ReadAll(limitReader)
	if err != nil {
		return "", err
	}
	if int64(len(body)) > maxBytes {
		body = body[:maxBytes]
		truncated = true
	}

	ct := resp.Header.Get("Content-Type")

	if downloadPath != "" {
		if truncated {
			return "", fmt.Errorf("download truncated (max_bytes=%d); increase tools.url_fetch.max_bytes or pass a larger max_bytes", maxBytes)
		}
		_, resolvedPath, err := resolveWritePath(t.FileCacheDir, downloadPath)
		if err != nil {
			return "", err
		}
		if downloadMkdirs {
			dir := filepath.Dir(resolvedPath)
			if dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return "", err
				}
			}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if err := os.WriteFile(resolvedPath, body, 0o644); err != nil {
				return "", err
			}
		}

		out, _ := json.MarshalIndent(map[string]any{
			"url":          sanitizeOutputURL(u.String()),
			"method":       method,
			"status":       resp.StatusCode,
			"content_type": ct,
			"bytes":        len(body),
			"path":         downloadPath,
			"abs_path":     resolvedPath,
		}, "", "  ")
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return string(out), fmt.Errorf("non-2xx status: %d", resp.StatusCode)
		}
		return string(out), nil
	}

	bodyStr := string(bytes.ToValidUTF8(body, []byte("\n[non-utf8 body]\n")))
	bodyStr = redactResponseBody(bodyStr)

	var b strings.Builder
	fmt.Fprintf(&b, "url: %s\n", sanitizeOutputURL(u.String()))
	fmt.Fprintf(&b, "method: %s\n", method)
	fmt.Fprintf(&b, "status: %d\n", resp.StatusCode)
	if ct != "" {
		fmt.Fprintf(&b, "content_type: %s\n", ct)
	}
	fmt.Fprintf(&b, "truncated: %t\n", truncated)
	b.WriteString("body:\n")
	b.WriteString(bodyStr)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b.String(), fmt.Errorf("non-2xx status: %d", resp.StatusCode)
	}
	return b.String(), nil
}

func formatInjectedSecret(format string, secret string) (string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", fmt.Errorf("resolved secret is empty")
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "raw":
		return secret, nil
	case "bearer":
		return "Bearer " + secret, nil
	case "basic":
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(secret)), nil
	default:
		return "", fmt.Errorf("unsupported inject.format: %q", format)
	}
}

func canonicalOrigin(u *url.URL) string {
	if u == nil {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	port := u.Port()
	if strings.TrimSpace(port) == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return scheme + "://" + host + ":" + port
}

func cloneDefaultTransport() *http.Transport {
	if dt, ok := http.DefaultTransport.(*http.Transport); ok && dt != nil {
		return dt.Clone()
	}
	return &http.Transport{}
}

var allowedUserHeaderNames = map[string]bool{
	"accept":            true,
	"content-type":      true,
	"user-agent":        true,
	"if-none-match":     true,
	"if-modified-since": true,
	"range":             true,
}

func isAllowedUserHeader(name string, authProfileID string, binding secrets.ToolBinding) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	if allowedUserHeaderNames[key] {
		return true
	}
	if authProfileID == "" || !binding.AllowUserHeaders {
		return false
	}
	for _, extra := range binding.UserHeaderAllowlist {
		if strings.EqualFold(strings.TrimSpace(extra), name) {
			return true
		}
	}
	return false
}

func normalizeHeaderKeyForPolicy(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func isDeniedUserHeader(name string) bool {
	raw := strings.ToLower(strings.TrimSpace(name))
	if raw == "" {
		return true
	}
	if strings.HasPrefix(raw, "proxy-") {
		return true
	}
	if strings.HasPrefix(raw, "x-forwarded-") {
		return true
	}

	n := normalizeHeaderKeyForPolicy(name)
	switch n {
	case "authorization", "cookie", "setcookie", "host", "proxyauthorization":
		return true
	}
	if strings.Contains(n, "apikey") {
		return true
	}
	if strings.Contains(n, "token") {
		return true
	}
	return false
}

func sanitizeOutputURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	changed := false
	for k := range q {
		if isSensitiveKeyLike(k) {
			q.Set(k, "[redacted]")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func isSensitiveKeyLike(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	n := strings.ReplaceAll(strings.ReplaceAll(k, "-", ""), "_", "")
	switch {
	case strings.Contains(n, "apikey"):
		return true
	case strings.Contains(n, "authorization"):
		return true
	case strings.Contains(n, "token"):
		return true
	case strings.Contains(n, "secret"):
		return true
	case strings.Contains(n, "password"):
		return true
	}
	return false
}

var (
	jwtLikeRe         = regexp.MustCompile(`(?m)\b[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)
	bearerLineRe      = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._-]{10,}\b`)
	privateKeyBlockRe = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
)

func redactResponseBody(body string) string {
	if strings.TrimSpace(body) == "" {
		return body
	}
	redacted := privateKeyBlockRe.ReplaceAllString(body, "-----BEGIN PRIVATE KEY-----\n[redacted]\n-----END PRIVATE KEY-----")
	redacted = jwtLikeRe.ReplaceAllString(redacted, "[redacted_jwt]")
	redacted = bearerLineRe.ReplaceAllString(redacted, "Bearer [redacted]")
	return redactSimpleKeyValue(redacted)
}

func redactSimpleKeyValue(s string) string {
	re := regexp.MustCompile(`(?i)\b([A-Za-z0-9_-]{1,32})(\s*[:=]\s*)([A-Za-z0-9._-]{12,})`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		if !isSensitiveKeyLike(sub[1]) {
			return m
		}
		return sub[1] + sub[2] + "[redacted]"
	})
}

func findExistingAbsPath(v any) string {
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return ""
		}
		if filepath.IsAbs(s) {
			if _, err := os.Stat(s); err == nil {
				return s
			}
		}
		return ""
	case map[string]any:
		for _, vv := range x {
			if p := findExistingAbsPath(vv); p != "" {
				return p
			}
		}
		return ""
	case []any:
		for _, vv := range x {
			if p := findExistingAbsPath(vv); p != "" {
				return p
			}
		}
		return ""
	default:
		return ""
	}
}

