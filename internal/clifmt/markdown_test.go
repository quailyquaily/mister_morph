package clifmt

import (
	"strings"
	"testing"
)

func TestRenderMarkdownHeading(t *testing.T) {
	input := "# Hello\n\nSome text"
	out := renderMarkdown(input, true)
	if strings.Contains(out, "#") {
		t.Fatalf("heading should not contain #, got: %q", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Fatalf("expected heading text, got: %q", out)
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

func TestRenderMarkdownTable(t *testing.T) {
	input := "| Name | Value |\n|------|-------|\n| foo  | bar   |"
	out := renderMarkdown(input, true)
	// Should contain box-drawing separators, not raw markdown syntax.
	if strings.Contains(out, "---") {
		t.Fatalf("raw markdown separator leaked into output: %q", out)
	}
	if !strings.Contains(out, "─┼─") {
		t.Fatalf("expected box-drawing separator, got: %q", out)
	}
	if !strings.Contains(out, "foo") || !strings.Contains(out, "bar") {
		t.Fatalf("expected cell contents, got: %q", out)
	}
}

func TestRenderMarkdownTableCJK(t *testing.T) {
	input := "| 项目 | 详情 |\n|------|------|\n| 名称 | gocli |"
	out := renderMarkdown(input, true)
	if strings.Contains(out, "---") {
		t.Fatalf("raw markdown separator leaked into output: %q", out)
	}
	if !strings.Contains(out, "项目") || !strings.Contains(out, "名称") {
		t.Fatalf("expected CJK cell contents, got: %q", out)
	}
}

func TestRenderMarkdownTablePipeInCode(t *testing.T) {
	input := "| Command | Usage |\n|---------|-------|\n| calc | `gocli calc <add|sub>` |"
	out := renderMarkdown(input, true)
	// The pipe inside the inline code should not split the cell.
	if !strings.Contains(out, "add|sub") {
		t.Fatalf("expected add|sub in a single cell, got: %q", out)
	}
	// Make sure we still have exactly two columns (no stray third column).
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.Contains(line, "│") {
			colCount := strings.Count(line, "│") - 1
			if colCount != 2 {
				t.Fatalf("expected 2 columns, got %d in line: %q", colCount, line)
			}
		}
	}
}

func TestRenderMarkdownTildeFencedCodeBlockPreservesPipe(t *testing.T) {
	input := "~~~go\nfmt.Println(`a|b`)\n~~~"
	out := renderMarkdown(input, false)
	if strings.Contains(out, "\x01\x02\x03") {
		t.Fatalf("pipe placeholder leaked into output: %q", out)
	}
	if !strings.Contains(out, "a|b") {
		t.Fatalf("expected a|b inside code block, got: %q", out)
	}
}

func TestRenderMarkdownTableDoubleBacktickPipe(t *testing.T) {
	input := "| Expr |\n|------|\n| ``a|b`` |"
	out := renderMarkdown(input, false)
	// The pipe inside ``a|b`` should stay intact and not split the cell.
	if !strings.Contains(out, "a|b") {
		t.Fatalf("expected a|b in table cell, got: %q", out)
	}
	// Make sure we still have exactly one column.
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if strings.Contains(line, "│") {
			colCount := strings.Count(line, "│") - 1
			if colCount != 1 {
				t.Fatalf("expected 1 column, got %d in line: %q", colCount, line)
			}
		}
	}
}

func TestRenderMarkdownBlockquoteMultiParagraph(t *testing.T) {
	input := "> quote\n>\n> next"
	out := renderMarkdown(input, false)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	for _, line := range lines {
		// Each line should have exactly one "│ " prefix.
		if strings.Count(line, "│") != 1 {
			t.Fatalf("expected exactly one │ per line, got: %q", line)
		}
	}
}
