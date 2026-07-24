package builtin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
)

func TestReaderAllowlist(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "X post", raw: "https://x.com/user/status/123", want: true},
		{name: "Twitter post", raw: "https://twitter.com/user/status/123", want: true},
		{name: "t.co short link", raw: "https://t.co/abc", want: true},
		{name: "Reddit comments", raw: "https://www.reddit.com/r/golang/comments/abc/example/", want: true},
		{name: "Reddit short link", raw: "https://redd.it/abc", want: true},
		{name: "ChatGPT share", raw: "https://chatgpt.com/share/123", want: true},
		{name: "Claude share", raw: "https://claude.ai/share/123", want: true},
		{name: "Gemini share", raw: "https://g.co/gemini/share/123", want: true},
		{name: "Grok share", raw: "https://grok.com/share/123", want: true},
		{name: "LinkedIn post", raw: "https://www.linkedin.com/posts/user_example-activity-123", want: true},
		{name: "LinkedIn feed update", raw: "https://www.linkedin.com/feed/update/urn:li:activity:123", want: true},
		{name: "Threads post", raw: "https://www.threads.net/@user/post/ABC", want: true},
		{name: "YouTube watch", raw: "https://www.youtube.com/watch?v=abc", want: true},
		{name: "YouTube short link", raw: "https://youtu.be/abc", want: true},
		{name: "Bilibili video", raw: "https://www.bilibili.com/video/BV1xx411c7mD", want: true},
		{name: "Medium article", raw: "https://medium.com/@user/article-123", want: true},
		{name: "Medium publication", raw: "https://publication.medium.com/article-123", want: true},
		{name: "Substack article", raw: "https://writer.substack.com/p/article", want: true},
		{name: "Substack note", raw: "https://substack.com/@writer/note/c-123", want: true},

		{name: "ordinary URL", raw: "https://example.com/article", want: false},
		{name: "X profile", raw: "https://x.com/user", want: false},
		{name: "Reddit homepage", raw: "https://www.reddit.com/", want: false},
		{name: "private ChatGPT chat", raw: "https://chatgpt.com/c/123", want: false},
		{name: "empty ChatGPT share", raw: "https://chatgpt.com/share/", want: false},
		{name: "private Claude chat", raw: "https://claude.ai/chat/123", want: false},
		{name: "empty Reddit comments", raw: "https://www.reddit.com/r/golang/comments/", want: false},
		{name: "LinkedIn profile", raw: "https://www.linkedin.com/in/user", want: false},
		{name: "YouTube homepage", raw: "https://www.youtube.com/", want: false},
		{name: "empty YouTube short", raw: "https://www.youtube.com/shorts/", want: false},
		{name: "empty Bilibili video", raw: "https://www.bilibili.com/video/BV", want: false},
		{name: "Medium homepage", raw: "https://medium.com/", want: false},
		{name: "Substack homepage", raw: "https://writer.substack.com/", want: false},
		{name: "lookalike domain", raw: "https://medium.com.example.test/article", want: false},
		{name: "non-HTTPS", raw: "http://medium.com/@user/article", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := url.Parse(tt.raw)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", tt.raw, err)
			}
			if got := isReaderAllowlistedURL(parsed); got != tt.want {
				t.Fatalf("isReaderAllowlistedURL(%q) = %t, want %t", tt.raw, got, tt.want)
			}
		})
	}
}

func TestURLFetchTool_XPostUsesDefuddle(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{
			name: "x.com",
			url:  "https://x.com/geekbb/status/2080516574683775418",
		},
		{
			name: "twitter.com",
			url:  "https://twitter.com/geekbb/status/2080516574683775418",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requested []string
			rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requested = append(requested, r.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/markdown; charset=utf-8"},
					},
					Body:    io.NopCloser(strings.NewReader("---\ntitle: post\n---\npost body")),
					Request: r,
				}, nil
			})

			tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
			tool.HTTPClient = &http.Client{Transport: rt}
			out, err := tool.Execute(context.Background(), map[string]any{"url": tt.url})
			if err != nil {
				t.Fatalf("Execute() error = %v (out=%q)", err, out)
			}

			wantURL := "https://defuddle.md/" + tt.url
			if len(requested) != 1 || requested[0] != wantURL {
				t.Fatalf("requested URLs = %#v, want [%q]", requested, wantURL)
			}
			if !strings.Contains(out, "url: "+tt.url) {
				t.Fatalf("Execute() output = %q, want original URL", out)
			}
			if strings.Contains(out, "reader:") {
				t.Fatalf("Execute() output = %q, should not expose reader implementation details", out)
			}
			if !strings.Contains(out, "post body") {
				t.Fatalf("Execute() output = %q, want post body", out)
			}
		})
	}
}

func TestURLFetchTool_AllowlistedURLUsesDefuddle(t *testing.T) {
	const target = "https://www.reddit.com/r/golang/comments/abc/example/"
	var requested []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/markdown; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader("reddit post body")),
			Request: r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{"url": target})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}

	wantURL := "https://defuddle.md/" + target
	if len(requested) != 1 || requested[0] != wantURL {
		t.Fatalf("requested URLs = %#v, want [%q]", requested, wantURL)
	}
	if strings.Contains(out, "reader:") || !strings.Contains(out, "reddit post body") {
		t.Fatalf("Execute() output = %q, want Defuddle response", out)
	}
}

func TestURLFetchTool_XPostFallsBackToJina(t *testing.T) {
	const target = "https://x.com/geekbb/status/2080516574683775418"
	var requested []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		status := http.StatusForbidden
		body := "defuddle blocked"
		if r.URL.Host == "r.jina.ai" {
			status = http.StatusOK
			body = "jina post body"
		}
		return &http.Response{
			StatusCode: status,
			Header: http.Header{
				"Content-Type": []string{"text/plain; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader(body)),
			Request: r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{"url": target})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}

	want := []string{
		"https://defuddle.md/" + target,
		"https://r.jina.ai/" + target,
	}
	if len(requested) != len(want) {
		t.Fatalf("requested URLs = %#v, want %#v", requested, want)
	}
	for i := range want {
		if requested[i] != want[i] {
			t.Fatalf("requested URLs = %#v, want %#v", requested, want)
		}
	}
	if strings.Contains(out, "reader:") || !strings.Contains(out, "jina post body") {
		t.Fatalf("Execute() output = %q, want Jina response without reader metadata", out)
	}
}

func TestURLFetchTool_XPostFallsBackToJinaAfterTransportError(t *testing.T) {
	const target = "https://x.com/geekbb/status/2080516574683775418"
	var requested []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		if r.URL.Host == "defuddle.md" {
			return nil, errors.New("defuddle unavailable")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/plain; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader("jina post body")),
			Request: r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{"url": target})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}
	if len(requested) != 2 ||
		requested[0] != "https://defuddle.md/"+target ||
		requested[1] != "https://r.jina.ai/"+target {
		t.Fatalf("requested URLs = %#v, want Defuddle then Jina", requested)
	}
	if strings.Contains(out, "reader:") || !strings.Contains(out, "jina post body") {
		t.Fatalf("Execute() output = %q, want Jina response without reader metadata", out)
	}
}

func TestURLFetchTool_XPostFallsBackToDirectAfterReadersFail(t *testing.T) {
	const target = "https://x.com/geekbb/status/2080516574683775418"
	var requested []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		status := http.StatusForbidden
		body := "defuddle blocked"
		switch r.URL.Host {
		case "r.jina.ai":
			status = http.StatusTooManyRequests
			body = "jina rate limited"
		case "x.com":
			status = http.StatusOK
			body = "direct post body"
		}
		return &http.Response{
			StatusCode: status,
			Header: http.Header{
				"Content-Type": []string{"text/plain; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader(body)),
			Request: r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{"url": target})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}
	want := []string{
		"https://defuddle.md/" + target,
		"https://r.jina.ai/" + target,
		target,
	}
	if len(requested) != len(want) {
		t.Fatalf("requested URLs = %#v, want %#v", requested, want)
	}
	for i := range want {
		if requested[i] != want[i] {
			t.Fatalf("requested URLs = %#v, want %#v", requested, want)
		}
	}
	if strings.Contains(out, "reader:") || !strings.Contains(out, "direct post body") {
		t.Fatalf("Execute() output = %q, want direct fallback response", out)
	}
}

func TestURLFetchTool_TCOUsesDefuddle(t *testing.T) {
	const shortURL = "https://t.co/example"
	var requested []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/markdown"},
			},
			Body:    io.NopCloser(strings.NewReader("resolved post")),
			Request: r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{"url": shortURL})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}

	wantURL := "https://defuddle.md/" + shortURL
	if len(requested) != 1 || requested[0] != wantURL {
		t.Fatalf("requested URLs = %#v, want [%q]", requested, wantURL)
	}
	if !strings.Contains(out, "url: "+shortURL) || !strings.Contains(out, "resolved post") {
		t.Fatalf("Execute() output = %q, want short URL and resolved post", out)
	}
}

func TestURLFetchTool_ReaderProxyOnlyAppliesToPlainXGET(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
	}{
		{
			name: "ordinary URL",
			params: map[string]any{
				"url": "https://example.test/article",
			},
		},
		{
			name: "lookalike X host",
			params: map[string]any{
				"url": "https://x.com.example.test/geekbb/status/2080516574683775418",
			},
		},
		{
			name: "non-status X URL",
			params: map[string]any{
				"url": "https://x.com/geekbb",
			},
		},
		{
			name: "insecure X URL",
			params: map[string]any{
				"url": "http://x.com/geekbb/status/2080516574683775418",
			},
		},
		{
			name: "X request with headers",
			params: map[string]any{
				"url": "https://x.com/geekbb/status/2080516574683775418",
				"headers": map[string]any{
					"Accept": "application/json",
				},
			},
		},
		{
			name: "X POST",
			params: map[string]any{
				"url":    "https://x.com/geekbb/status/2080516574683775418",
				"method": http.MethodPost,
				"body":   "payload",
			},
		},
		{
			name: "X download",
			params: map[string]any{
				"url":           "https://x.com/geekbb/status/2080516574683775418",
				"download_path": "post.txt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requested []string
			rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requested = append(requested, r.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/plain"},
					},
					Body:    io.NopCloser(strings.NewReader("direct")),
					Request: r,
				}, nil
			})

			tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
			tool.HTTPClient = &http.Client{Transport: rt}
			out, err := tool.Execute(context.Background(), tt.params)
			if err != nil {
				t.Fatalf("Execute() error = %v (out=%q)", err, out)
			}

			wantURL := tt.params["url"].(string)
			if len(requested) != 1 || requested[0] != wantURL {
				t.Fatalf("requested URLs = %#v, want direct request to %q", requested, wantURL)
			}
		})
	}
}

func TestURLFetchTool_ReaderDestinationsDeniedByGuardFallBackToDirect(t *testing.T) {
	const target = "https://x.com/geekbb/status/2080516574683775418"
	calls := 0
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("direct post")),
			Request:    r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	ctx := guard.WithNetworkPolicy(context.Background(), guard.NetworkPolicy{
		AllowedURLPrefixes: []string{target},
	})
	out, err := tool.Execute(ctx, map[string]any{"url": target})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}
	if calls != 1 {
		t.Fatalf("RoundTrip calls = %d, want one direct fallback request", calls)
	}
	if !strings.Contains(out, "direct post") {
		t.Fatalf("Execute() output = %q, want direct response", out)
	}
}
