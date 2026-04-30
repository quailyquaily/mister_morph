package clifmt

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/mattn/go-runewidth"
)

var codeBlockRe = regexp.MustCompile("(?s)```(\\w*)\\n(.*?)```")
var ansiRe = regexp.MustCompile("(?s)\x1b\\[[0-9;]*[mK]")

// HighlightCodeBlocks finds markdown code blocks in text and applies syntax highlighting,
// borders, and line numbers. It also handles plain code (without markdown fences) by
// auto-detecting code-like content.
func HighlightCodeBlocks(text string) string {
	if !useColor() {
		return text
	}

	// First, handle explicit markdown code blocks
	hasCodeBlocks := codeBlockRe.MatchString(text)
	if hasCodeBlocks {
		return codeBlockRe.ReplaceAllStringFunc(text, func(block string) string {
			matches := codeBlockRe.FindStringSubmatch(block)
			if len(matches) != 3 {
				return block
			}
			lang := strings.TrimSpace(matches[1])
			code := matches[2]
			highlighted, err := highlightCode(code, lang)
			if err != nil {
				return block
			}
			return "\n" + wrapInBox(highlighted, lang) + "\n"
		})
	}

	// No markdown code blocks found - check if the entire text looks like code
	if looksLikeCode(text) {
		highlighted, err := highlightCode(text, "")
		if err != nil {
			return text
		}
		return "\n" + wrapInBox(highlighted, "") + "\n"
	}

	return text
}

// looksLikeCodeBlock checks if a specific text segment looks like source code.
// It is stricter than looksLikeCode and requires at least 3 lines.
func looksLikeCodeBlock(text string) bool {
	lines := strings.Split(text, "\n")
	if len(lines) < 3 {
		return false
	}
	return looksLikeCode(text)
}

func highlightCode(src, language string) (string, error) {
	var lexer chroma.Lexer
	if language != "" {
		lexer = lexers.Get(language)
	}
	if lexer == nil {
		lexer = lexers.Analyse(src)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}

	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	iterator, err := lexer.Tokenise(nil, src)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func wrapInBox(highlighted string, lang string) string {
	lines := strings.Split(strings.TrimRight(highlighted, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	// Calculate max width for the box
	maxWidth := 0
	for _, line := range lines {
		w := visibleWidth(line)
		if w > maxWidth {
			maxWidth = w
		}
	}

	// Line number gutter width
	gutterWidth := len(fmt.Sprintf("%d", len(lines)))
	if gutterWidth < 2 {
		gutterWidth = 2
	}

	header := lang
	if header == "" {
		header = "code"
	}

	// Total inner width of the box (excluding the outer 1-char borders on each side)
	// |  gutter  | content |
	// totalInnerWidth = (2 for left padding) + (gutterWidth) + (3 for gutter divider separator) + (maxWidth) + (2 for right padding)
	totalInnerWidth := 2 + gutterWidth + 3 + maxWidth + 2

	var b strings.Builder

	// Top border
	b.WriteString("\x1b[90m┌── \x1b[0m")
	b.WriteString("\x1b[1;36m" + header + "\x1b[0m")
	topLineLen := totalInnerWidth - 3 - visibleWidth(header)
	if topLineLen < 2 {
		topLineLen = 2
	}
	b.WriteString("\x1b[90m " + strings.Repeat("─", topLineLen) + "┐\x1b[0m\n")

	// Content with line numbers and side borders
	for i, line := range lines {
		lineNum := i + 1
		padding := maxWidth - visibleWidth(line)

		// Check for diff markers to color the gutter
		cleanLine := stripANSI(line)
		gutterColor := "\x1b[90m" // dim gray default
		if strings.HasPrefix(cleanLine, "+") {
			gutterColor = "\x1b[32m" // green
		} else if strings.HasPrefix(cleanLine, "-") {
			gutterColor = "\x1b[31m" // red
		}

		b.WriteString("\x1b[90m│ \x1b[0m")
		b.WriteString(fmt.Sprintf("%s%*d │ \x1b[0m", gutterColor, gutterWidth, lineNum))
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", padding))
		b.WriteString("\x1b[90m │\x1b[0m\n")
	}

	// Bottom border
	b.WriteString("\x1b[90m└" + strings.Repeat("─", totalInnerWidth) + "┘\x1b[0m")

	return b.String()
}

func isMarkdownHeader(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	// Must be "# " or "## " etc. up to 6 levels
	for i := 1; i <= 6; i++ {
		prefix := strings.Repeat("#", i) + " "
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func visibleWidth(s string) int {
	stripped := stripANSI(s)
	width := 0
	for _, r := range stripped {
		if r == '\t' {
			width += 8 - (width % 8)
		} else {
			width += runewidth.RuneWidth(r)
		}
	}
	return width
}

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// looksLikeCode heuristically detects if plain text is source code.
func looksLikeCode(text string) bool {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return false
	}

	codeIndicators := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Skip markdown headers (e.g., # Title, ## Subtitle)
		if isMarkdownHeader(trimmed) {
			continue
		}

		// Skip Chinese-only sentences (plain text explanation)
		if isChineseSentence(trimmed) {
			continue
		}

		if strings.HasPrefix(trimmed, "def ") ||
			strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "import ") ||
			strings.HasPrefix(trimmed, "from ") ||
			strings.HasPrefix(trimmed, "function ") ||
			strings.HasPrefix(trimmed, "const ") ||
			strings.HasPrefix(trimmed, "let ") ||
			strings.HasPrefix(trimmed, "var ") ||
			strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "/*") ||
			strings.HasPrefix(trimmed, "*") ||
			strings.Contains(line, "    ") ||
			strings.Contains(line, "\t") ||
			(strings.Contains(line, "(") && strings.Contains(line, ")")) ||
			(strings.Contains(line, "{") && strings.Contains(line, "}")) ||
			(strings.Contains(line, "=") && !strings.Contains(line, "==")) {
			codeIndicators++
		}
	}

	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	return nonEmpty > 0 && float64(codeIndicators)/float64(nonEmpty) > 0.3
}

// isChineseSentence checks if a line is a Chinese sentence (plain text, not code).
func isChineseSentence(line string) bool {
	// Count Chinese characters and ASCII code symbols
	chineseCount := 0
	codeSymbolCount := 0
	for _, r := range line {
		if r >= 0x4e00 && r <= 0x9fff {
			chineseCount++
		}
		if strings.ContainsRune("(){}[];:=+-*/%<>!&|", r) {
			codeSymbolCount++
		}
	}
	// If mostly Chinese characters and few code symbols, it's a sentence
	if chineseCount >= 3 && codeSymbolCount == 0 {
		return true
	}
	return false
}
