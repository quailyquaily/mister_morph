package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type URLFetchTool struct {
	Enabled     bool
	Timeout     time.Duration
	MaxBytes    int64
	UserAgent   string
	HTTPClient  *http.Client
	AllowScheme map[string]bool
}

func NewURLFetchTool(enabled bool, timeout time.Duration, maxBytes int64, userAgent string) *URLFetchTool {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "mister_morph/1.0 (+https://github.com/quailyquaily)"
	}
	return &URLFetchTool{
		Enabled:   enabled,
		Timeout:   timeout,
		MaxBytes:  maxBytes,
		UserAgent: userAgent,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
		AllowScheme: map[string]bool{"http": true, "https": true},
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
			"headers": map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"type": "string",
				},
				"description": "Optional HTTP headers to send. Values must be strings.",
			},
			"body": map[string]any{
				"type":        []string{"string", "object", "array", "number", "boolean", "null"},
				"description": "Optional request body (supported for POST and PUT). For more complex cases (other methods, multipart, binary), use the bash tool with curl.",
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

	var hasUserAgent bool
	var hasContentType bool
	if hdrs, ok := params["headers"]; ok && hdrs != nil {
		m, ok := hdrs.(map[string]any)
		if !ok {
			return "", fmt.Errorf("invalid param: headers must be an object of string values (for more complex requests, use the bash tool with curl)")
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
			if strings.EqualFold(key, "host") {
				if value != "" {
					req.Host = value
				}
				continue
			}
			// Disallow setting Content-Length via headers; net/http will compute it.
			if strings.EqualFold(key, "content-length") {
				continue
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

	var client http.Client
	if t.HTTPClient != nil {
		client = *t.HTTPClient
	}
	client.Timeout = timeout

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

	var b strings.Builder
	fmt.Fprintf(&b, "url: %s\n", u.String())
	fmt.Fprintf(&b, "method: %s\n", method)
	fmt.Fprintf(&b, "status: %d\n", resp.StatusCode)
	if ct != "" {
		fmt.Fprintf(&b, "content_type: %s\n", ct)
	}
	fmt.Fprintf(&b, "truncated: %t\n", truncated)
	b.WriteString("body:\n")
	b.WriteString(string(bytes.ToValidUTF8(body, []byte("\n[non-utf8 body]\n"))))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b.String(), fmt.Errorf("non-2xx status: %d", resp.StatusCode)
	}
	return b.String(), nil
}
