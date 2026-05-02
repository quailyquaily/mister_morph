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
	// Expand tabs before tokenising so the formatter never emits raw tab
	// characters, which terminals render as a jump to the next tab stop and
	// can misalign indented code inside boxed or guttered output.
	src = strings.ReplaceAll(src, "\t", "    ")

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

	gutterWidth := len(fmt.Sprintf("%d", len(lines)))
	if gutterWidth < 3 {
		gutterWidth = 3
	}

	header := lang
	if header == "" {
		header = "code"
	}

	gray := "\x1b[38;5;245m"
	bg := "\x1b[48;5;235m" // dark grey background for code blocks

	var b strings.Builder
	// Header line
	b.WriteString(bg)
	b.WriteString(gray)
	b.WriteString(header)
	b.WriteString("\x1b[K")
	b.WriteString("\x1b[0m")
	b.WriteByte('\n')

	for i, line := range lines {
		lineNum := i + 1
		// Gutter: background + grey line number
		b.WriteString(bg)
		b.WriteString(gray)
		b.WriteString(fmt.Sprintf("%*d", gutterWidth, lineNum))
		b.WriteString("\x1b[39m") // reset foreground only, keep background
		b.WriteString("  ")
		// Code: strip any existing bg colours, then make \x1b[0m only
		// reset the foreground so the code-block bg stays active.
		safe := ansiBgRe.ReplaceAllString(line, "")
		safe = strings.ReplaceAll(safe, "\x1b[0m", "\x1b[39m"+bg)
		safe = reapplyBgBeforeWideChars(safe, bg)
		b.WriteString(safe)
		b.WriteString("\x1b[K")
		b.WriteString("\x1b[0m")
		b.WriteByte('\n')
	}

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

		// High-confidence code signatures only.
		if strings.HasPrefix(trimmed, "def ") ||
			strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "import ") ||
			strings.HasPrefix(trimmed, "from ") ||
			strings.HasPrefix(trimmed, "function ") ||
			strings.HasPrefix(trimmed, "const ") ||
			strings.HasPrefix(trimmed, "let ") ||
			strings.HasPrefix(trimmed, "var ") ||
			strings.HasPrefix(trimmed, "package ") ||
			strings.HasPrefix(trimmed, "func ") ||
			strings.HasPrefix(trimmed, "struct ") ||
			strings.HasPrefix(trimmed, "type ") ||
			strings.HasPrefix(trimmed, "if ") ||
			strings.HasPrefix(trimmed, "for ") ||
			strings.HasPrefix(trimmed, "while ") ||
			strings.HasPrefix(trimmed, "return ") ||
			strings.HasPrefix(trimmed, "public ") ||
			strings.HasPrefix(trimmed, "private ") ||
			strings.HasPrefix(trimmed, "async ") ||
			strings.HasPrefix(trimmed, "await ") ||
			strings.HasPrefix(trimmed, "try ") ||
			strings.HasPrefix(trimmed, "catch ") ||
			strings.HasPrefix(trimmed, "throw ") ||
			strings.HasPrefix(trimmed, "new ") ||
			strings.HasPrefix(trimmed, "else ") ||
			strings.HasPrefix(trimmed, "elif ") ||
			strings.HasPrefix(trimmed, "print ") ||
			strings.HasPrefix(trimmed, "fmt.") ||
			strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "/*") ||
			strings.HasSuffix(trimmed, ";") ||
			strings.HasSuffix(trimmed, "{") ||
			strings.HasSuffix(trimmed, "}") {
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
