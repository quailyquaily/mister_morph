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

	"golang.org/x/net/html"
)

type WebSearchTool struct {
	Enabled        bool
	BaseURL        string
	Timeout        time.Duration
	MaxResults     int
	UserAgent      string
	MaxBodyBytes   int64
	AllowRedirects bool
}

func (t *WebSearchTool) ParallelSafe() bool { return true }

func NewWebSearchTool(enabled bool, baseURL string, timeout time.Duration, maxResults int, userAgent string) *WebSearchTool {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://duckduckgo.com/html/"
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if maxResults <= 0 {
		maxResults = 5
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "mistermorph/1.0 (+https://github.com/quailyquaily)"
	}

	return &WebSearchTool{
		Enabled:        enabled,
		BaseURL:        baseURL,
		Timeout:        timeout,
		MaxResults:     maxResults,
		UserAgent:      userAgent,
		MaxBodyBytes:   2 * 1024 * 1024,
		AllowRedirects: true,
	}
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web for a query and return a short list of results (title, url, snippet)."
}

func (t *WebSearchTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q": map[string]any{
				"type":        "string",
				"description": "Search query.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Optional max results to return.",
			},
		},
		"required": []string{"q"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

func (t *WebSearchTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if !t.Enabled {
		return "", fmt.Errorf("web_search tool is disabled (enable via config: tools.web_search.enabled=true)")
	}

	q, _ := params["q"].(string)
	q = strings.TrimSpace(q)
	if q == "" {
		return "", fmt.Errorf("missing required param: q")
	}

	maxResults := t.MaxResults
	if v, ok := params["max_results"]; ok {
		if n, ok := asInt64(v); ok && n > 0 {
			maxResults = int(n)
		}
	}
	if maxResults > 20 {
		maxResults = 20
	}

	base, err := url.Parse(t.BaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	u := *base
	qs := u.Query()
	qs.Set("q", q)
	u.RawQuery = qs.Encode()

	httpClient := &http.Client{Timeout: t.Timeout}
	if !t.AllowRedirects {
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", t.UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		return "", fmt.Errorf("web_search non-2xx status=%d body=%s", resp.StatusCode, string(bytes.ToValidUTF8(body, []byte("[non-utf8]"))))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, t.MaxBodyBytes))
	if err != nil {
		return "", err
	}

	results, err := parseDuckDuckGoHTML(body, maxResults)
	if err != nil {
		return "", err
	}

	out := map[string]any{
		"engine":       "duckduckgo_html",
		"query":        q,
		"result_count": len(results),
		"results":      results,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

func parseDuckDuckGoHTML(htmlBytes []byte, maxResults int) ([]webSearchResult, error) {
	root, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return nil, err
	}

	var out []webSearchResult

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n == nil || len(out) >= maxResults {
			return
		}

		// Result title links tend to be: <a class="result__a" href="...">Title</a>
		if n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "result__a") {
			href := attr(n, "href")
			title := strings.TrimSpace(textContent(n))
			if href != "" && title != "" {
				res := webSearchResult{
					Title: title,
					URL:   normalizeDuckDuckGoResultURL(href),
				}
				out = append(out, res)
			}
		}

		for c := n.FirstChild; c != nil && len(out) < maxResults; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	// Snippets are harder to pair without full result block traversal; keep minimal but stable.
	return out, nil
}

func normalizeDuckDuckGoResultURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}

	// Often: /l/?uddg=<encoded>
	u, err := url.Parse(href)
	if err != nil {
		return href
	}

	if u.Path == "/l/" {
		uddg := u.Query().Get("uddg")
		if uddg != "" {
			decoded, err := url.QueryUnescape(uddg)
			if err == nil && decoded != "" {
				return decoded
			}
		}
	}

	if u.Scheme == "" && strings.HasPrefix(href, "//") {
		return "https:" + href
	}

	return href
}

func hasClass(n *html.Node, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	class := attr(n, "class")
	for _, part := range strings.Fields(class) {
		if part == want {
			return true
		}
	}
	return false
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x == nil {
			return
		}
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
