package clifmt

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// ansiBgRe matches ANSI escape sequences that set or reset background colors.
var ansiBgRe = regexp.MustCompile("\x1b\\[(?:4[0-8]|10[0-7]|49)(?:;[^m]*)?m")

// DiffLine represents a single line in a unified diff output.
type diffLine struct {
	kind   byte // ' ' context, '-' delete, '+' insert
	text   string
	oldNum int // 0 if not present in old file
	newNum int // 0 if not present in new file
}

// splitLines splits a string into lines, discarding the final empty element
// that strings.Split produces when text ends with a newline.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// lineDiff computes a line-level diff between oldContent and newContent.
func lineDiff(oldContent, newContent string) []diffLine {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	// Encode each unique line as a single Unicode private-use-area character
	// so we can run diffmatchpatch at line granularity reliably.
	lineToChar := make(map[string]rune)
	charToLine := make(map[rune]string)
	nextChar := rune(0xE000)

	getChar := func(line string) rune {
		if c, ok := lineToChar[line]; ok {
			return c
		}
		c := nextChar
		nextChar++
		lineToChar[line] = c
		charToLine[c] = line
		return c
	}

	var oldChars, newChars []rune
	for _, line := range oldLines {
		oldChars = append(oldChars, getChar(line))
	}
	for _, line := range newLines {
		newChars = append(newChars, getChar(line))
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(string(oldChars), string(newChars), false)
	diffs = dmp.DiffCleanupSemantic(diffs)

	var result []diffLine
	oldNum := 0
	newNum := 0

	for _, diff := range diffs {
		for _, c := range diff.Text {
			line := charToLine[c]
			switch diff.Type {
			case diffmatchpatch.DiffEqual:
				oldNum++
				newNum++
				result = append(result, diffLine{kind: ' ', text: line, oldNum: oldNum, newNum: newNum})
			case diffmatchpatch.DiffDelete:
				oldNum++
				result = append(result, diffLine{kind: '-', text: line, oldNum: oldNum, newNum: 0})
			case diffmatchpatch.DiffInsert:
				newNum++
				result = append(result, diffLine{kind: '+', text: line, oldNum: 0, newNum: newNum})
			}
		}
	}

	return result
}

// foldContext folds unchanged lines that are farther than `context` lines away
// from any change, replacing them with a single fold marker line.
func foldContext(lines []diffLine, context int) []diffLine {
	var changedIndices []int
	for i, dl := range lines {
		if dl.kind != ' ' {
			changedIndices = append(changedIndices, i)
		}
	}

	if len(changedIndices) == 0 {
		return nil
	}

	visible := make(map[int]bool, len(lines))
	for _, idx := range changedIndices {
		for j := idx - context; j <= idx+context; j++ {
			if j >= 0 && j < len(lines) {
				visible[j] = true
			}
		}
	}

	var result []diffLine
	lastVisible := -1
	for i := 0; i < len(lines); i++ {
		if visible[i] {
			if (lastVisible != -1 && i > lastVisible+1) || (lastVisible == -1 && i > 0) {
				// Insert a single fold marker for the gap.
				result = append(result, diffLine{kind: 0})
			}
			result = append(result, lines[i])
			lastVisible = i
		}
	}

	return result
}

func extToLang(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".jsx":
		return "jsx"
	case ".tsx":
		return "tsx"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc", ".cxx":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".sh", ".bash":
		return "bash"
	case ".zsh":
		return "zsh"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".r":
		return "r"
	case ".lua":
		return "lua"
	case ".vim":
		return "vim"
	case ".dockerfile":
		return "dockerfile"
	default:
		return ""
	}
}

// RenderDiff renders a terminal-friendly unified diff between oldContent and newContent.
// It shows a single line-number column, highlights additions/deletions with full-width
// background color, applies syntax highlighting to code, and folds long stretches of
// unchanged context.
func RenderDiff(path, oldContent, newContent string) string {
	lines := lineDiff(oldContent, newContent)
	if len(lines) == 0 {
		return ""
	}

	folded := foldContext(lines, 3)

	// Compute gutter width based on the largest line number.
	maxNum := 0
	for _, dl := range lines {
		if dl.oldNum > maxNum {
			maxNum = dl.oldNum
		}
		if dl.newNum > maxNum {
			maxNum = dl.newNum
		}
	}
	gutterWidth := len(fmt.Sprintf("%d", maxNum))
	if gutterWidth < 3 {
		gutterWidth = 3
	}

	color := useColor()

	// Syntax highlighting by language (only when color is enabled).
	lang := extToLang(filepath.Ext(path))
	oldHL := make(map[int]string)
	newHL := make(map[int]string)
	if color && lang != "" {
		if oldContent != "" {
			if highlighted, err := highlightCode(oldContent, lang); err == nil {
				hlLines := strings.Split(strings.TrimRight(highlighted, "\n"), "\n")
				for i, line := range hlLines {
					oldHL[i+1] = line
				}
			}
		}
		if newContent != "" {
			if highlighted, err := highlightCode(newContent, lang); err == nil {
				hlLines := strings.Split(strings.TrimRight(highlighted, "\n"), "\n")
				for i, line := range hlLines {
					newHL[i+1] = line
				}
			}
		}
	}

	var b strings.Builder

	gray := ""
	if color {
		gray = "\x1b[38;5;250m"
	}

	// File header
	if color {
		b.WriteString(fmt.Sprintf("%s%s\x1b[0m\n", gray, path))
	} else {
		b.WriteString(path + "\n")
	}

	for _, dl := range folded {
		if dl.kind == 0 {
			continue
		}

		// Determine line number and highlighted text.
		var lineNum int
		var text string
		switch dl.kind {
		case '-':
			lineNum = dl.oldNum
			if hl, ok := oldHL[dl.oldNum]; ok {
				text = hl
			} else {
				text = dl.text
			}
		case '+':
			lineNum = dl.newNum
			if hl, ok := newHL[dl.newNum]; ok {
				text = hl
			} else {
				text = dl.text
			}
		default:
			lineNum = dl.newNum
			if hl, ok := newHL[dl.newNum]; ok {
				text = hl
			} else {
				text = dl.text
			}
		}

		switch dl.kind {
		case '-':
			if color {
				bg := "\x1b[48;2;61;1;0m"
				fg := "\x1b[38;5;174m"
				b.WriteString(bg)
				b.WriteString(fmt.Sprintf("%s%*d%s - ", gray, gutterWidth, lineNum, fg))
				safeText := strings.ReplaceAll(text, "\x1b[0m", "\x1b[39m"+bg+fg)
				safeText = ansiBgRe.ReplaceAllString(safeText, "")
				b.WriteString(safeText)
				b.WriteString(bg)
				b.WriteString("\x1b[K\x1b[0m")
			} else {
				b.WriteString(fmt.Sprintf("%*d - %s", gutterWidth, lineNum, text))
			}
		case '+':
			if color {
				bg := "\x1b[48;2;2;40;0m"
				fg := "\x1b[38;5;108m"
				b.WriteString(bg)
				b.WriteString(fmt.Sprintf("%s%*d%s + ", gray, gutterWidth, lineNum, fg))
				safeText := strings.ReplaceAll(text, "\x1b[0m", "\x1b[39m"+bg+fg)
				safeText = ansiBgRe.ReplaceAllString(safeText, "")
				b.WriteString(safeText)
				b.WriteString(bg)
				b.WriteString("\x1b[K\x1b[0m")
			} else {
				b.WriteString(fmt.Sprintf("%*d + %s", gutterWidth, lineNum, text))
			}
		default:
			if color {
				b.WriteString(fmt.Sprintf("%s%*d\x1b[0m  ", gray, gutterWidth, lineNum))
			} else {
				b.WriteString(fmt.Sprintf("%*d  ", gutterWidth, lineNum))
			}
			b.WriteString(text)
		}
		b.WriteByte('\n')
	}

	return b.String()
}
