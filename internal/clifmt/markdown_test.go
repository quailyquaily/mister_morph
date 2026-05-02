package clifmt

import (
	"strings"
	"testing"
)

func TestRenderMarkdownHeading(t *testing.T) {
	input := "# Hello\n\nSome text"
	out := renderMarkdown(input, true)
	if !strings.Contains(out, "# Hello") {
		t.Fatalf("expected heading, got: %q", out)
	}
}

func TestRenderMarkdownBold(t *testing.T) {
	input := "This is **bold** text"
	out := renderMarkdown(input, true)
	if !strings.Contains(out, "bold") {
		t.Fatalf("expected bold text, got: %q", out)
	}
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	input := "```go\npackage main\n```"
	out := renderMarkdown(input, true)
	if !strings.Contains(out, "package") || !strings.Contains(out, "main") {
		t.Fatalf("expected code block, got: %q", out)
	}
}

func TestRenderMarkdownList(t *testing.T) {
	input := "- item one\n- item two"
	out := renderMarkdown(input, true)
	if !strings.Contains(out, "item one") || !strings.Contains(out, "item two") {
		t.Fatalf("expected list items, got: %q", out)
	}
}

func TestRenderMarkdownLink(t *testing.T) {
	input := "[link text](https://example.com)"
	out := renderMarkdown(input, true)
	if !strings.Contains(out, "link text") {
		t.Fatalf("expected link text, got: %q", out)
	}
}

func TestRenderMarkdownImage(t *testing.T) {
	input := "![alt text](https://example.com/img.png)"
	out := renderMarkdown(input, true)
	if !strings.Contains(out, "[image]") {
		t.Fatalf("expected image placeholder, got: %q", out)
	}
}

func TestRenderMarkdownBlockquote(t *testing.T) {
	input := "> quote"
	out := renderMarkdown(input, true)
	if !strings.Contains(out, "│") {
		t.Fatalf("expected blockquote prefix, got: %q", out)
	}
}
