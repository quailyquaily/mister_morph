package builtin

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const weChatArticleUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/119.0"

var weChatCreateTimePattern = regexp.MustCompile(`(?m)\bvar\s+createTime\s*=\s*['"]([^'"]+)['"]\s*;`)

type weChatArticle struct {
	Title       string
	Description string
	CoverImage  string
	Channel     string
	PublishedAt string
	Markdown    string
}

func isWeChatArticleURL(u *url.URL) bool {
	if u == nil || u.User != nil || !strings.EqualFold(strings.TrimSpace(u.Scheme), "https") {
		return false
	}
	if port := strings.TrimSpace(u.Port()); port != "" && port != "443" {
		return false
	}
	if !strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(u.Hostname()), "."), "mp.weixin.qq.com") {
		return false
	}
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	return path == "/s" || strings.HasPrefix(path, "/s/")
}

func extractWeChatArticle(body []byte, base *url.URL, maxBytes int) (weChatArticle, bool) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return weChatArticle{}, false
	}
	content := findWeChatElementByID(doc, "js_content")
	if content == nil {
		return weChatArticle{}, false
	}

	markdown := renderWeChatMarkdown(content, base, maxBytes)
	if strings.TrimSpace(markdown) == "" {
		return weChatArticle{}, false
	}

	title := weChatMetaContent(doc, "og:title")
	if title == "" {
		title = weChatElementText(findWeChatElementByID(doc, "activity-name"))
	}
	publishedAt := weChatElementText(findWeChatElementByID(doc, "publish_time"))
	if publishedAt == "" {
		if matches := weChatCreateTimePattern.FindSubmatch(body); len(matches) == 2 {
			publishedAt = string(matches[1])
		}
	}

	coverImage := resolveWeChatResource(weChatMetaContent(doc, "og:image"), base)
	return weChatArticle{
		Title:       normalizeWeChatMetadata(title),
		Description: normalizeWeChatMetadata(weChatMetaContent(doc, "og:description")),
		CoverImage:  sanitizeOutputURL(coverImage),
		Channel:     normalizeWeChatMetadata(weChatElementText(findWeChatElementByID(doc, "js_name"))),
		PublishedAt: normalizeWeChatMetadata(publishedAt),
		Markdown:    markdown,
	}, true
}

func appendWeChatArticleOutput(b *strings.Builder, article weChatArticle) {
	b.WriteString("extracted: true\n")
	b.WriteString("source_type: wechat_article\n")
	if article.Title != "" {
		fmt.Fprintf(b, "title: %s\n", article.Title)
	}
	if article.Channel != "" {
		fmt.Fprintf(b, "channel: %s\n", article.Channel)
	}
	if article.PublishedAt != "" {
		fmt.Fprintf(b, "published_at: %s\n", article.PublishedAt)
	}
	if article.Description != "" {
		fmt.Fprintf(b, "description: %s\n", article.Description)
	}
	if article.CoverImage != "" {
		fmt.Fprintf(b, "cover_image: %s\n", article.CoverImage)
	}
	b.WriteString("body_markdown:\n")
	b.WriteString(article.Markdown)
}

func findWeChatElementByID(root *html.Node, id string) *html.Node {
	id = strings.TrimSpace(id)
	if root == nil || id == "" {
		return nil
	}
	var found *html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || found != nil {
			return
		}
		if node.Type == html.ElementNode && weChatNodeAttr(node, "id") == id {
			found = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func weChatMetaContent(root *html.Node, property string) string {
	property = strings.TrimSpace(property)
	if root == nil || property == "" {
		return ""
	}
	var content string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || content != "" {
			return
		}
		if node.Type == html.ElementNode &&
			strings.EqualFold(node.Data, "meta") &&
			strings.EqualFold(strings.TrimSpace(weChatNodeAttr(node, "property")), property) {
			content = strings.TrimSpace(weChatNodeAttr(node, "content"))
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return content
}

func weChatNodeAttr(node *html.Node, name string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

func weChatElementText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil || weChatNodeIgnored(current) {
			return
		}
		if current.Type == html.TextNode {
			text := strings.Join(strings.Fields(current.Data), " ")
			if text != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(text)
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(b.String()), " ")
}

func normalizeWeChatMetadata(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func renderWeChatMarkdown(content *html.Node, base *url.URL, maxBytes int) string {
	if content == nil {
		return ""
	}
	var b strings.Builder
	for child := content.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(renderWeChatMarkdownNode(child, base))
	}
	out := normalizeWeChatMarkdown(b.String())
	if maxBytes > 0 && len(out) > maxBytes {
		out = out[:maxBytes]
		for !utf8.ValidString(out) && len(out) > 0 {
			out = out[:len(out)-1]
		}
		out = strings.TrimSpace(out)
	}
	return out
}

func renderWeChatMarkdownNode(node *html.Node, base *url.URL) string {
	if node == nil || weChatNodeIgnored(node) {
		return ""
	}
	if node.Type == html.TextNode {
		return normalizeWeChatTextNode(node.Data)
	}
	if node.Type != html.ElementNode {
		return renderWeChatMarkdownChildren(node, base)
	}

	tag := strings.ToLower(node.Data)
	switch tag {
	case "br":
		return "\n"
	case "img":
		src := strings.TrimSpace(weChatNodeAttr(node, "data-src"))
		if src == "" {
			src = strings.TrimSpace(weChatNodeAttr(node, "src"))
		}
		src = sanitizeOutputURL(resolveWeChatResource(src, base))
		if src == "" {
			return ""
		}
		alt := normalizeWeChatMetadata(weChatNodeAttr(node, "alt"))
		return "![" + escapeWeChatMarkdownText(alt) + "](" + src + ")"
	case "a":
		text := strings.TrimSpace(renderWeChatMarkdownChildren(node, base))
		href := sanitizeOutputURL(resolveWeChatResource(weChatNodeAttr(node, "href"), base))
		if href == "" {
			return text
		}
		if text == "" {
			text = href
		}
		return "[" + text + "](" + href + ")"
	case "strong", "b":
		return wrapWeChatMarkdownInline("**", renderWeChatMarkdownChildren(node, base))
	case "em", "i":
		return wrapWeChatMarkdownInline("_", renderWeChatMarkdownChildren(node, base))
	case "del", "s", "strike":
		return wrapWeChatMarkdownInline("~~", renderWeChatMarkdownChildren(node, base))
	case "code":
		text := strings.TrimSpace(weChatElementText(node))
		if text == "" {
			return ""
		}
		return "`" + strings.ReplaceAll(text, "`", "\\`") + "`"
	case "pre":
		text := strings.TrimSpace(weChatRawText(node))
		if text == "" {
			return ""
		}
		fence := "```"
		if strings.Contains(text, fence) {
			fence = "````"
		}
		return "\n\n" + fence + "\n" + text + "\n" + fence + "\n\n"
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(tag[1] - '0')
		text := strings.TrimSpace(renderWeChatMarkdownChildren(node, base))
		if text == "" {
			return ""
		}
		return "\n\n" + strings.Repeat("#", level) + " " + text + "\n\n"
	case "p", "div", "section", "article", "header", "footer", "figure", "figcaption":
		text := strings.TrimSpace(renderWeChatMarkdownChildren(node, base))
		if text == "" {
			return ""
		}
		return "\n\n" + text + "\n\n"
	case "blockquote":
		text := normalizeWeChatMarkdown(renderWeChatMarkdownChildren(node, base))
		if text == "" {
			return ""
		}
		lines := strings.Split(text, "\n")
		for i := range lines {
			if lines[i] == "" {
				lines[i] = ">"
			} else {
				lines[i] = "> " + lines[i]
			}
		}
		return "\n\n" + strings.Join(lines, "\n") + "\n\n"
	case "ul":
		return renderWeChatList(node, base, false)
	case "ol":
		return renderWeChatList(node, base, true)
	default:
		return renderWeChatMarkdownChildren(node, base)
	}
}

func renderWeChatMarkdownChildren(node *html.Node, base *url.URL) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(renderWeChatMarkdownNode(child, base))
	}
	return b.String()
}

func renderWeChatList(node *html.Node, base *url.URL, ordered bool) string {
	var lines []string
	index := 1
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || !strings.EqualFold(child.Data, "li") || weChatNodeIgnored(child) {
			continue
		}
		text := normalizeWeChatMarkdown(renderWeChatMarkdownChildren(child, base))
		if text == "" {
			continue
		}
		prefix := "- "
		if ordered {
			prefix = fmt.Sprintf("%d. ", index)
		}
		itemLines := strings.Split(text, "\n")
		lines = append(lines, prefix+itemLines[0])
		for _, line := range itemLines[1:] {
			if line == "" {
				lines = append(lines, "")
			} else {
				lines = append(lines, "   "+line)
			}
		}
		index++
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(lines, "\n") + "\n\n"
}

func weChatNodeIgnored(node *html.Node) bool {
	if node == nil || node.Type != html.ElementNode {
		return false
	}
	switch strings.ToLower(node.Data) {
	case "script", "style", "noscript", "svg", "canvas", "iframe":
		return true
	}
	switch strings.TrimSpace(weChatNodeAttr(node, "id")) {
	case "js_pc_qr_code", "page_bottom_area", "meta_content":
		return true
	}
	for _, class := range strings.Fields(weChatNodeAttr(node, "class")) {
		switch class {
		case "appmsg_card_context", "mp_profile_iframe_wrp":
			return true
		}
	}
	return false
}

func normalizeWeChatTextNode(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	first, _ := utf8.DecodeRuneInString(value)
	leadingSpace := unicode.IsSpace(first)
	last, _ := utf8.DecodeLastRuneInString(value)
	trailingSpace := unicode.IsSpace(last)
	value = strings.Join(strings.Fields(value), " ")
	if leadingSpace {
		value = " " + value
	}
	if trailingSpace {
		value += " "
	}
	return value
}

func normalizeWeChatMarkdown(markdown string) string {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	markdown = strings.ReplaceAll(markdown, "\r", "\n")
	lines := strings.Split(markdown, "\n")
	cleaned := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if blank || len(cleaned) == 0 {
				continue
			}
			blank = true
			cleaned = append(cleaned, "")
			continue
		}
		blank = false
		cleaned = append(cleaned, line)
	}
	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	return strings.Join(cleaned, "\n")
}

func wrapWeChatMarkdownInline(marker string, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return marker + content + marker
}

func escapeWeChatMarkdownText(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`)
	return replacer.Replace(value)
}

func weChatRawText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil || weChatNodeIgnored(current) {
			return
		}
		if current.Type == html.TextNode {
			b.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func resolveWeChatResource(raw string, base *url.URL) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https":
		return parsed.String()
	default:
		return ""
	}
}
