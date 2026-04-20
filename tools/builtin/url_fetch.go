package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/secrets"
	"golang.org/x/net/html"
)

const (
	defaultURLFetchMaxBytesInline   int64 = 512 * 1024
	defaultURLFetchMaxBytesDownload int64 = 100 * 1024 * 1024
)

type URLFetchAuth struct {
	AllowProfiles map[string]bool
	Profiles      *secrets.ProfileStore
}

type URLFetchTool struct {
	Enabled          bool
	Timeout          time.Duration
	MaxBytes         int64
	MaxBytesDownload int64
	UserAgent        string
	HTTPClient       *http.Client
	AllowScheme      map[string]bool
	Auth             *URLFetchAuth
	FileCacheDir     string
}

func NewURLFetchTool(enabled bool, timeout time.Duration, maxBytes int64, userAgent string, fileCacheDir string) *URLFetchTool {
	return NewURLFetchToolWithAuth(enabled, timeout, maxBytes, userAgent, fileCacheDir, nil)
}

func NewURLFetchToolWithAuth(enabled bool, timeout time.Duration, maxBytes int64, userAgent string, fileCacheDir string, auth *URLFetchAuth) *URLFetchTool {
	return NewURLFetchToolWithAuthLimits(enabled, timeout, maxBytes, 0, userAgent, fileCacheDir, auth)
}

func NewURLFetchToolWithAuthLimits(enabled bool, timeout time.Duration, maxBytes int64, maxBytesDownload int64, userAgent string, fileCacheDir string, auth *URLFetchAuth) *URLFetchTool {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxBytes <= 0 {
		maxBytes = defaultURLFetchMaxBytesInline
	}
	if maxBytesDownload <= 0 {
		maxBytesDownload = defaultURLFetchMaxBytesDownload
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "mistermorph/1.0 (+https://github.com/quailyquaily)"
	}
	fileCacheDir = strings.TrimSpace(fileCacheDir)
	if fileCacheDir == "" {
		fileCacheDir = "~/.cache/morph"
	}
	return &URLFetchTool{
		Enabled:          enabled,
		Timeout:          timeout,
		MaxBytes:         maxBytes,
		MaxBytesDownload: maxBytesDownload,
		UserAgent:        userAgent,
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
	return "Fetches an URL and returns the response body (truncated). " +
		"For binary responses or files(`.pdf`, `.zip`, `.png`, `.jpg`, `.mp4`, etc), prefer `download_path` to save to a file instead of returning it's content. " +
		"If file type is unclear, you may first issue a small-range GET using a `Range` header with a low `max_bytes` to confirm content type before downloading. " +
		"If `url_fetch` fails (blocked, timeout, non-2xx), report the error."
}

func (t *URLFetchTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to fetch HTTP(S).",
			},
			"method": map[string]any{
				"type":        "string",
				"description": "Optional HTTP method. Defaults to GET.",
				"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
			},
			"auth_profile": map[string]any{
				"type":        "string",
				"description": "Optional auth profile id for credential injection.",
			},
			"headers": map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"type": "string",
				},
				"description": "Optional HTTP headers to send.",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Optional request body (supported for POST, PUT, PATCH). ",
			},
			"download_path": map[string]any{
				"type":        "string",
				"description": "Optional: if set, saves the raw response body to this path (under `file_cache_dir`) and returns JSON metadata instead of including the body in the output. Always use `download_path` for binary or non-text files.",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": "Optional timeout override in seconds.",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": "Optional max response bytes to read (truncates beyond this). ",
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
		if !urlAllowedByPrefixes(u.String(), netPol.AllowedURLPrefixes) {
			return "", fmt.Errorf("url is not allowed by guard")
		}
		if netPol.DenyPrivateIPs && isDeniedPrivateHost(u.Hostname()) {
			return "", fmt.Errorf("private ip/localhost is not allowed by guard")
		}
	}

	downloadPath, _ := params["download_path"].(string)
	downloadPath = strings.TrimSpace(downloadPath)

	method := http.MethodGet
	if v, ok := params["method"]; ok {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("invalid param: method must be a string (for more complex requests, use curl)")
		}
		s = strings.ToUpper(strings.TrimSpace(s))
		if s != "" {
			method = s
		}
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return "", fmt.Errorf("unsupported method: %s (url_fetch supports GET, POST, PUT, PATCH, DELETE; for other methods use curl)", method)
	}

	timeout := t.Timeout
	if v, ok := params["timeout_seconds"]; ok {
		if secs, ok := asFloat64(v); ok && secs > 0 {
			timeout = time.Duration(secs * float64(time.Second))
		}
	}

	maxBytes := t.MaxBytes
	if downloadPath != "" {
		maxBytes = t.MaxBytesDownload
	}
	if v, ok := params["max_bytes"]; ok {
		if n, ok := asInt64(v); ok && n > 0 {
			maxBytes = n
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var bodyReader io.Reader
	var bodyProvided bool
	var inferredContentType string
	if v, ok := params["body"]; ok {
		bodyProvided = true
		if v != nil {
			switch x := v.(type) {
			case string:
				bodyReader = strings.NewReader(x)
				inferredContentType = inferContentTypeFromStringBody(x)
			default:
				if authProfileID != "" {
					if offending := findExistingAbsPath(x); offending != "" {
						return "", fmt.Errorf("request body contains local file path %q; use read_file and send the file contents instead", offending)
					}
				}
				inferredContentType = "application/json"
				bodyBytes, err := json.Marshal(x)
				if err != nil {
					return "", fmt.Errorf("invalid param: body must be a string or JSON-serializable value (for more complex requests, use curl): %w", err)
				}
				bodyReader = bytes.NewReader(bodyBytes)
			}
		}
	}
	if bodyProvided && method != http.MethodPost && method != http.MethodPut {
		if method != http.MethodPatch {
			return "", fmt.Errorf("request body is only supported for POST/PUT/PATCH in url_fetch (use curl for %s with a body)", method)
		}
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
		if t.Auth == nil {
			return "", fmt.Errorf("auth_profile is not configured")
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

		sec := strings.TrimSpace(p.Credential.Secret)
		if sec == "" {
			return "", fmt.Errorf("auth_profile %q credential.secret is empty", authProfileID)
		}
		injectHeaderName = strings.TrimSpace(b.Inject.Name)
		injectHeaderVal, err = formatInjectedSecret(b.Inject.Format, sec)
		if err != nil {
			return "", err
		}
	}

	var hasUserAgent bool
	var hasContentType bool
	headersProvided := false
	if hdrs, ok := params["headers"]; ok && hdrs != nil {
		headersProvided = true
		m, ok := hdrs.(map[string]any)
		if !ok {
			return "", fmt.Errorf("invalid param: headers must be an object of string values (for more complex requests, use curl)")
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
				return "", fmt.Errorf("invalid header %q: value must be a string (for more complex requests, use curl)", key)
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
	// If caller did not provide headers at all, infer Content-Type from body type.
	if !headersProvided && !hasContentType && inferredContentType != "" {
		req.Header.Set("Content-Type", inferredContentType)
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
			if netPol.DenyPrivateIPs && isDeniedPrivateHost(req.URL.Hostname()) {
				return fmt.Errorf("private ip/localhost is not allowed by guard")
			}
			if !urlAllowedByPrefixes(req.URL.String(), netPol.AllowedURLPrefixes) {
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

	debugEnabled := slog.Default().Enabled(reqCtx, slog.LevelDebug)
	if debugEnabled {
		slog.DebugContext(reqCtx, "url_fetch_request_url", "url", sanitizeOutputURL(req.URL.String()))
		slog.DebugContext(reqCtx, "url_fetch_request_method", "method", method)
		slog.DebugContext(reqCtx, "url_fetch_request_headers", "headers", sanitizeHeadersForDebugLog(req.Header))
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
	sniffedCT := ""
	isHTML := isHTMLContentType(ct)
	if !isHTML {
		sniffedCT = http.DetectContentType(body)
		if isHTMLContentType(sniffedCT) {
			isHTML = true
			if ct == "" {
				ct = sniffedCT
			}
		} else if ct == "" && sniffedCT != "" {
			ct = sniffedCT
		}
	}

	if downloadPath != "" {
		if truncated {
			return "", fmt.Errorf("download truncated (max_bytes=%d); increase tools.url_fetch.max_bytes_download or pass a larger max_bytes", maxBytes)
		}
		_, resolvedPath, err := resolveWritePath(pathroots.New("", t.FileCacheDir, ""), downloadPath)
		if err != nil {
			return "", err
		}
		dir := filepath.Dir(resolvedPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", err
			}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if err := os.WriteFile(resolvedPath, body, 0o644); err != nil {
				return "", err
			}
		}

		saved := resp.StatusCode >= 200 && resp.StatusCode < 300
		out, _ := json.MarshalIndent(map[string]any{
			"url":          sanitizeOutputURL(u.String()),
			"method":       method,
			"status":       resp.StatusCode,
			"content_type": ct,
			"bytes":        len(body),
			"path":         downloadPath,
			"abs_path":     resolvedPath,
			"saved":        saved,
			"note":         "saved to file_cache_dir",
		}, "", "  ")
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return string(out), fmt.Errorf("non-2xx status: %d", resp.StatusCode)
		}
		return string(out), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "url: %s\n", sanitizeOutputURL(u.String()))
	fmt.Fprintf(&b, "method: %s\n", method)
	fmt.Fprintf(&b, "status: %d\n", resp.StatusCode)
	if ct != "" {
		fmt.Fprintf(&b, "content_type: %s\n", ct)
	}
	fmt.Fprintf(&b, "truncated: %t\n", truncated)
	if isHTML {
		title, text, links := extractHTMLText(body, clampInt(maxBytes), u)
		if strings.TrimSpace(text) != "" {
			fmt.Fprintf(&b, "extracted: true\n")
			if title != "" {
				fmt.Fprintf(&b, "title: %s\n", title)
			}
			b.WriteString("body_text:\n")
			b.WriteString(text)
			if len(links) > 0 {
				b.WriteString("\nlinks:\n")
				for _, link := range links {
					if strings.TrimSpace(link.Text) == "" {
						fmt.Fprintf(&b, "- %s\n", link.Href)
					} else {
						fmt.Fprintf(&b, "- %s (%s)\n", link.Text, link.Href)
					}
				}
			}
		} else {
			bodyStr := string(bytes.ToValidUTF8(body, []byte("\n[non-utf8 body]\n")))
			bodyStr = redactResponseBody(bodyStr)
			b.WriteString("body:\n")
			b.WriteString(bodyStr)
		}
	} else {
		bodyStr := string(bytes.ToValidUTF8(body, []byte("\n[non-utf8 body]\n")))
		bodyStr = redactResponseBody(bodyStr)
		b.WriteString("body:\n")
		b.WriteString(bodyStr)
	}

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

const (
	urlFetchDebugHeaderMaxChars = 1024
)

func sanitizeHeadersForDebugLog(headers http.Header) string {
	if len(headers) == 0 {
		return "{}"
	}
	safe := make(map[string]string, len(headers))
	for key, values := range headers {
		joined := strings.Join(values, ", ")
		if shouldRedactDebugHeader(key) {
			safe[key] = "[redacted]"
			continue
		}
		safe[key] = redactSimpleKeyValue(joined)
	}
	b, err := json.Marshal(safe)
	if err != nil {
		return "{}"
	}
	return truncateForDebugLog(string(b), urlFetchDebugHeaderMaxChars)
}

func truncateForDebugLog(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func shouldRedactDebugHeader(name string) bool {
	if isDeniedUserHeader(name) {
		return true
	}
	return isSensitiveKeyLike(name)
}

func inferContentTypeFromStringBody(body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "text/plain"
	}
	if json.Valid([]byte(trimmed)) {
		return "application/json"
	}
	return "text/plain"
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

func isHTMLContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		return false
	}
	if strings.Contains(ct, ";") {
		ct = strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])
	}
	return ct == "text/html" || ct == "application/xhtml+xml"
}

type extractedLink struct {
	Text string
	Href string
}

const maxExtractedLinks = 50

func extractHTMLText(body []byte, maxBytes int, base *url.URL) (string, string, []extractedLink) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", "", nil
	}

	skipTags := map[string]bool{
		"script":   true,
		"style":    true,
		"noscript": true,
		"svg":      true,
		"canvas":   true,
	}

	var title string
	var text strings.Builder
	links := make([]extractedLink, 0, 8)
	seenLinks := make(map[string]bool)

	var walk func(n *html.Node, skip bool)
	walk = func(n *html.Node, skip bool) {
		if n == nil {
			return
		}
		if maxBytes > 0 && text.Len() >= maxBytes {
			return
		}

		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			if tag == "title" && title == "" {
				title = extractNodeText(n, maxBytes)
			}
			if (tag == "a" || tag == "iframe") && len(links) < maxExtractedLinks {
				attrKey := "href"
				if tag == "iframe" {
					attrKey = "src"
				}
				href := ""
				for _, attr := range n.Attr {
					if strings.EqualFold(attr.Key, attrKey) {
						href = strings.TrimSpace(attr.Val)
						break
					}
				}
				if href != "" && !strings.HasPrefix(strings.ToLower(href), "javascript:") {
					resolved := resolveLink(href, base)
					if resolved != "" && !seenLinks[resolved] {
						seenLinks[resolved] = true
						textVal := extractNodeText(n, maxBytes)
						links = append(links, extractedLink{
							Text: strings.Join(strings.Fields(textVal), " "),
							Href: resolved,
						})
					}
				}
			}
			if skipTags[tag] {
				skip = true
			}
		}

		if n.Type == html.TextNode && !skip {
			seg := strings.TrimSpace(n.Data)
			if seg != "" {
				if text.Len() > 0 {
					text.WriteByte(' ')
				}
				text.WriteString(seg)
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, skip)
			if maxBytes > 0 && text.Len() >= maxBytes {
				return
			}
		}
	}

	walk(doc, false)

	title = strings.Join(strings.Fields(title), " ")
	bodyText := strings.Join(strings.Fields(text.String()), " ")
	if maxBytes > 0 && len(bodyText) > maxBytes {
		bodyText = bodyText[:maxBytes]
	}
	return title, bodyText, links
}

func extractNodeText(n *html.Node, maxBytes int) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}
		if node.Type == html.TextNode {
			seg := strings.TrimSpace(node.Data)
			if seg != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(seg)
			}
		}
		if maxBytes > 0 && b.Len() >= maxBytes {
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if maxBytes > 0 && b.Len() >= maxBytes {
				return
			}
		}
	}
	walk(n)
	out := strings.Join(strings.Fields(b.String()), " ")
	if maxBytes > 0 && len(out) > maxBytes {
		out = out[:maxBytes]
	}
	return out
}

func resolveLink(href string, base *url.URL) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base == nil {
		return parsed.String()
	}
	return base.ResolveReference(parsed).String()
}

func clampInt(v int64) int {
	if v <= 0 {
		return 0
	}
	if v > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(v)
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

func urlAllowedByPrefixes(raw string, prefixes []string) bool {
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

func isDeniedPrivateHost(host string) bool {
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
