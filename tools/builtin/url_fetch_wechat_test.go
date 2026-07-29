package builtin

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestIsWeChatArticleURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "query article", raw: "https://mp.weixin.qq.com/s?__biz=example", want: true},
		{name: "path article", raw: "https://mp.weixin.qq.com/s/example", want: true},
		{name: "HTTP", raw: "http://mp.weixin.qq.com/s/example", want: false},
		{name: "custom port", raw: "https://mp.weixin.qq.com:8443/s/example", want: false},
		{name: "lookalike host", raw: "https://mp.weixin.qq.com.example.test/s/example", want: false},
		{name: "homepage", raw: "https://mp.weixin.qq.com/", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := url.Parse(tt.raw)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", tt.raw, err)
			}
			if got := isWeChatArticleURL(parsed); got != tt.want {
				t.Fatalf("isWeChatArticleURL(%q) = %t, want %t", tt.raw, got, tt.want)
			}
		})
	}
}

func TestURLFetchToolExtractsWeChatArticleAsMarkdown(t *testing.T) {
	const target = "https://mp.weixin.qq.com/s/example"
	const page = `<!doctype html>
<html>
<head>
  <title>fallback page title</title>
  <meta property="og:title" content="测试标题">
  <meta property="og:description" content="文章摘要">
  <meta property="og:image" content="https://mmbiz.qpic.cn/cover">
  <script>var createTime = '2026-07-29 09:45';</script>
</head>
<body>
  <div id="js_name">测试公众号</div>
  <div id="js_content">
    <section>
      <h2>第一节</h2>
      <p>第一段 <strong>重点</strong> 和 <a href="/next">链接</a>。</p>
      <p><img src="data:image/gif;base64,placeholder" data-src="https://mmbiz.qpic.cn/article-image" alt="配图"></p>
      <ul><li>条目一</li><li>条目二</li></ul>
    </section>
    <div id="page_bottom_area">不应出现的页脚</div>
    <div class="appmsg_card_context">不应出现的卡片</div>
  </div>
</body>
</html>`

	var requestedURLs []string
	var requestedUserAgents []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestedURLs = append(requestedURLs, r.URL.String())
		requestedUserAgents = append(requestedUserAgents, r.Header.Get("User-Agent"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/html; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader(page)),
			Request: r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 16*1024, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{"url": target})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}

	if len(requestedURLs) != 1 || requestedURLs[0] != target {
		t.Fatalf("requested URLs = %#v, want one direct request to %q", requestedURLs, target)
	}
	const wantUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/119.0"
	if len(requestedUserAgents) != 1 || requestedUserAgents[0] != wantUserAgent {
		t.Fatalf("requested User-Agents = %#v, want %q", requestedUserAgents, wantUserAgent)
	}
	for _, want := range []string{
		"source_type: wechat_article",
		"title: 测试标题",
		"channel: 测试公众号",
		"published_at: 2026-07-29 09:45",
		"description: 文章摘要",
		"cover_image: https://mmbiz.qpic.cn/cover",
		"body_markdown:\n## 第一节",
		"第一段 **重点** 和 [链接](https://mp.weixin.qq.com/next)。",
		"![配图](https://mmbiz.qpic.cn/article-image)",
		"- 条目一",
		"- 条目二",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Execute() output = %q, want %q", out, want)
		}
	}
	for _, unwanted := range []string{
		"fallback page title",
		"不应出现的页脚",
		"不应出现的卡片",
		"data:image/gif",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("Execute() output contains %q: %q", unwanted, out)
		}
	}
}

func TestURLFetchToolFallsBackForUnrecognizedWeChatHTML(t *testing.T) {
	const target = "https://mp.weixin.qq.com/s/example"
	const page = `<html><head><title>错误页</title></head><body>访问过于频繁，请稍后再试。</body></html>`

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/html; charset=utf-8"},
			},
			Body:    io.NopCloser(strings.NewReader(page)),
			Request: r,
		}, nil
	})

	tool := NewURLFetchTool(true, 2*time.Second, 4096, "test-agent", t.TempDir())
	tool.HTTPClient = &http.Client{Transport: rt}
	out, err := tool.Execute(context.Background(), map[string]any{"url": target})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}
	if strings.Contains(out, "source_type: wechat_article") {
		t.Fatalf("Execute() output misclassified error page: %q", out)
	}
	if !strings.Contains(out, "body_text:") || !strings.Contains(out, "访问过于频繁") {
		t.Fatalf("Execute() output = %q, want generic HTML extraction", out)
	}
}
