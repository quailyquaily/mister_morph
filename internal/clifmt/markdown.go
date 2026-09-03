package clifmt

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// terminalRenderer renders a goldmark AST to terminal-friendly output with
// ANSI color codes.
type terminalRenderer struct {
	color      bool
	styleStack []string

	// tableColWidths holds the pre-computed display width for each column
	// in the current table (including 1-space padding on each side).
	// Populated when entering a Table node and cleared when exiting.
	tableColWidths []int
	// width is the maximum rendered line width. Zero means unconstrained.
	width int
}

func newTerminalRenderer(color bool, width int) *terminalRenderer {
	return &terminalRenderer{color: color, width: width}
}

func (r *terminalRenderer) pushStyle(code string) {
	r.styleStack = append(r.styleStack, code)
}

func (r *terminalRenderer) popStyle() string {
	if len(r.styleStack) == 0 {
		return ""
	}
	last := len(r.styleStack) - 1
	code := r.styleStack[last]
	r.styleStack = r.styleStack[:last]
	return code
}

func (r *terminalRenderer) currentStyles() string {
	var b strings.Builder
	for _, code := range r.styleStack {
		b.WriteString(code)
	}
	return b.String()
}

func (r *terminalRenderer) applyStyles(w util.BufWriter) {
	if !r.color {
		return
	}
	styles := r.currentStyles()
	if styles != "" {
		w.WriteString(styles)
	}
}

func (r *terminalRenderer) closeStyle(w util.BufWriter) {
	if !r.color {
		return
	}
	r.popStyle()
	w.WriteString("\x1b[0m")
	r.applyStyles(w)
}

// blockEnter writes a leading newline when node is a top-level block that
// follows another block. This ensures exactly one blank line between adjacent
// block-level elements without every renderer having to manage spacing on both
// sides.
func (r *terminalRenderer) blockEnter(w util.BufWriter, node ast.Node) {
	if node.PreviousSibling() != nil && node.Parent() != nil && node.Parent().Kind() == ast.KindDocument {
		w.WriteString("\n")
	}
}

// writeBlockquotePrefix writes the gray "│ " prefix when node is a direct
// child of a Blockquote (or when a ListItem's parent List is a direct child).
func (r *terminalRenderer) writeBlockquotePrefix(w util.BufWriter, node ast.Node) {
	isInBlockquote := false
	parent := node.Parent()
	if parent != nil && parent.Kind() == ast.KindBlockquote {
		isInBlockquote = true
	}
	if !isInBlockquote && parent != nil && parent.Kind() == ast.KindList {
		grandparent := parent.Parent()
		if grandparent != nil && grandparent.Kind() == ast.KindBlockquote {
			isInBlockquote = true
		}
	}
	if !isInBlockquote {
		return
	}
	if r.color {
		w.WriteString("\x1b[38;5;245m│ \x1b[0m")
	} else {
		w.WriteString("│ ")
	}
}

// pipePlaceholder is a Private Use Area character used to temporarily replace
// literal '|' characters inside inline code spans within table rows. Goldmark's
// table parser treats every '|' as a column separator even when it appears
// inside backticks, so we mask it during parsing and restore it during render.
const pipePlaceholder = "\x01\x02\x03"

// preprocessTableRows escapes '|' characters inside inline code spans (`...`)
// within lines that contain a table delimiter, preventing goldmark's table
// parser from splitting the cell at the pipe.
func preprocessTableRows(text string) string {
	lines := strings.Split(text, "\n")
	inCodeBlock := false
	var fenceChar byte
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) >= 3 {
			c := trimmed[0]
			if c == '`' || c == '~' {
				j := 0
				for j < len(trimmed) && trimmed[j] == c {
					j++
				}
				if j >= 3 {
					if !inCodeBlock {
						inCodeBlock = true
						fenceChar = c
					} else if c == fenceChar {
						inCodeBlock = false
						fenceChar = 0
					}
					continue
				}
			}
		}
		if inCodeBlock || !strings.Contains(line, "|") {
			continue
		}
		// Skip indented code blocks (4+ leading spaces).
		if len(line) >= 4 && line[0] == ' ' && line[1] == ' ' && line[2] == ' ' && line[3] == ' ' {
			continue
		}
		lines[i] = escapePipesInInlineCode(line)
	}
	return strings.Join(lines, "\n")
}

// escapePipesInInlineCode replaces '|' characters that appear inside Markdown
// inline code spans. It respects matching backtick-run delimiters, so “a|b“
// is handled correctly in addition to `a|b`.
func escapePipesInInlineCode(line string) string {
	var b strings.Builder
	b.Grow(len(line))
	i := 0
	for i < len(line) {
		if line[i] != '`' {
			b.WriteByte(line[i])
			i++
			continue
		}
		// Count the opening backtick run.
		start := i
		for i < len(line) && line[i] == '`' {
			i++
		}
		tickLen := i - start

		// Search for a closing run of the same length.
		j := i
		for j < len(line) {
			if line[j] != '`' {
				j++
				continue
			}
			runStart := j
			for j < len(line) && line[j] == '`' {
				j++
			}
			if j-runStart == tickLen {
				inner := line[i:runStart]
				inner = strings.ReplaceAll(inner, "|", pipePlaceholder)
				b.WriteString(line[start:i])    // opening backticks
				b.WriteString(inner)            // content with pipes escaped
				b.WriteString(line[runStart:j]) // closing backticks
				i = j
				break
			}
		}
		if j >= len(line) {
			// No matching close — write remainder unchanged.
			b.WriteString(line[start:])
			break
		}
	}
	return b.String()
}

// getCellText extracts the raw text content from a TableCell node by walking
// its children and accumulating Text node segments.
func getCellText(cell ast.Node, source []byte) string {
	var b strings.Builder
	ast.Walk(cell, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindText:
			text := string(n.(*ast.Text).Segment.Value(source))
			text = strings.ReplaceAll(text, pipePlaceholder, "|")
			b.WriteString(text)
		case ast.KindImage:
			b.WriteString("[image]")
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

func (r *terminalRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindDocument, r.renderDocument)
	reg.Register(ast.KindHeading, r.renderHeading)
	reg.Register(ast.KindParagraph, r.renderParagraph)
	reg.Register(ast.KindText, r.renderText)
	reg.Register(ast.KindEmphasis, r.renderEmphasis)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
	reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
	reg.Register(ast.KindCodeSpan, r.renderCodeSpan)
	reg.Register(ast.KindList, r.renderList)
	reg.Register(ast.KindListItem, r.renderListItem)
	reg.Register(ast.KindBlockquote, r.renderBlockquote)
	reg.Register(ast.KindLink, r.renderLink)
	reg.Register(ast.KindImage, r.renderImage)
	reg.Register(ast.KindThematicBreak, r.renderThematicBreak)
	reg.Register(extast.KindTable, r.renderTable)
	reg.Register(extast.KindTableHeader, r.renderTableHeader)
	reg.Register(extast.KindTableRow, r.renderTableRow)
}

func (r *terminalRenderer) renderDocument(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.blockEnter(w, node)
		r.writeBlockquotePrefix(w, node)
		if r.color {
			r.pushStyle("\x1b[1m")
			_, _ = w.WriteString("\x1b[1m")
		}
	} else {
		if r.color {
			r.closeStyle(w)
		}
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderParagraph(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.blockEnter(w, node)
		r.writeBlockquotePrefix(w, node)
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("\n")
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Text)
	text := string(n.Segment.Value(source))
	text = strings.ReplaceAll(text, pipePlaceholder, "|")
	_, _ = w.WriteString(text)
	if n.HardLineBreak() {
		_, _ = w.WriteString("\n")
	} else if n.SoftLineBreak() {
		_, _ = w.WriteString(" ")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderEmphasis(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !r.color {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Emphasis)
	if entering {
		if n.Level >= 2 {
			r.pushStyle("\x1b[1m")
			_, _ = w.WriteString("\x1b[1m")
		} else {
			r.pushStyle("\x1b[3m")
			_, _ = w.WriteString("\x1b[3m")
		}
	} else {
		r.closeStyle(w)
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderFencedCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	r.blockEnter(w, node)
	r.writeBlockquotePrefix(w, node)
	n := node.(*ast.FencedCodeBlock)

	var codeBuf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = codeBuf.Write(line.Value(source))
	}
	code := codeBuf.String()

	if !r.color {
		_, _ = w.WriteString(code)
		return ast.WalkSkipChildren, nil
	}

	lang := ""
	if n.Info != nil {
		lang = strings.TrimSpace(string(n.Info.Text(source)))
	}
	highlighted, err := highlightCode(code, lang)
	if err != nil {
		_, _ = w.WriteString(code)
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(wrapInBox(highlighted, lang))
	return ast.WalkSkipChildren, nil
}

func (r *terminalRenderer) renderCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	r.blockEnter(w, node)
	r.writeBlockquotePrefix(w, node)
	n := node.(*ast.CodeBlock)

	var codeBuf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = codeBuf.Write(line.Value(source))
	}
	code := codeBuf.String()

	if !r.color {
		_, _ = w.WriteString(code)
		return ast.WalkSkipChildren, nil
	}

	highlighted, err := highlightCode(code, "")
	if err != nil {
		_, _ = w.WriteString(code)
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(wrapInBox(highlighted, ""))
	return ast.WalkSkipChildren, nil
}

func (r *terminalRenderer) renderCodeSpan(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !r.color {
		return ast.WalkContinue, nil
	}
	if entering {
		r.pushStyle("\x1b[38;2;177;185;249m")
		_, _ = w.WriteString("\x1b[38;2;177;185;249m")
	} else {
		r.closeStyle(w)
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.blockEnter(w, node)
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderListItem(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		if node.PreviousSibling() != nil {
			_, _ = w.WriteString("\n")
		}
		r.writeBlockquotePrefix(w, node)
		parent := node.Parent()
		var prefix string
		if parent != nil && parent.Kind() == ast.KindList {
			list := parent.(*ast.List)
			if list.IsOrdered() {
				idx := 1
				for prev := node.PreviousSibling(); prev != nil; prev = prev.PreviousSibling() {
					if prev.Kind() == ast.KindListItem {
						idx++
					}
				}
				start := list.Start
				if start < 1 {
					start = 1
				}
				prefix = fmt.Sprintf("  %d. ", start+idx-1)
			} else {
				prefix = "  • "
			}
		} else {
			prefix = "  • "
		}
		if r.color {
			_, _ = w.WriteString("\x1b[38;5;245m")
			_, _ = w.WriteString(prefix)
			_, _ = w.WriteString("\x1b[0m")
		} else {
			_, _ = w.WriteString(prefix)
		}
	} else {
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderBlockquote(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.blockEnter(w, node)
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("[image]")
	}
	return ast.WalkSkipChildren, nil
}

func (r *terminalRenderer) renderThematicBreak(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.blockEnter(w, node)
		r.writeBlockquotePrefix(w, node)
		width := getTermWidth()
		if width <= 0 {
			width = 40
		}
		_, _ = w.WriteString(strings.Repeat("─", width))
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

// drawTableBorder writes a horizontal border line: prefix + dashes per column
// with intersection chars between columns + suffix.
func drawTableBorder(w util.BufWriter, widths []int, left, cross, right string) {
	if len(widths) == 0 {
		return
	}
	_, _ = w.WriteString(left)
	for i, width := range widths {
		if i > 0 {
			_, _ = w.WriteString(cross)
		}
		_, _ = w.WriteString(strings.Repeat("─", width))
	}
	_, _ = w.WriteString(right)
	_, _ = w.WriteString("\n")
}

func tableWidth(widths []int) int {
	width := len(widths) + 1 // outer borders and column separators
	for _, columnWidth := range widths {
		width += columnWidth
	}
	return width
}

func fitTableColumnWidths(natural []int, maximum int) []int {
	if maximum <= 0 || tableWidth(natural) <= maximum || len(natural) == 0 {
		return natural
	}

	available := maximum - len(natural) - 1
	if available < len(natural) {
		return natural
	}

	widths := make([]int, len(natural))
	for idx := range widths {
		widths[idx] = 1
	}
	remaining := available - len(widths)
	for remaining > 0 {
		active := 0
		for idx := range widths {
			if widths[idx] < natural[idx] {
				active++
			}
		}
		if active == 0 {
			break
		}

		share := max(1, remaining/active)
		for idx := range widths {
			if widths[idx] >= natural[idx] || remaining == 0 {
				continue
			}
			increase := min(share, natural[idx]-widths[idx], remaining)
			widths[idx] += increase
			remaining -= increase
		}
	}
	return widths
}

func tableCells(node ast.Node) []ast.Node {
	cells := make([]ast.Node, 0, 4)
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Kind() == extast.KindTableCell {
			cells = append(cells, child)
		}
	}
	return cells
}

func tableCellLayout(width int) (leftPadding, contentWidth, rightPadding int) {
	switch {
	case width >= 3:
		return 1, width - 2, 1
	case width == 2:
		return 0, 1, 1
	default:
		return 0, 1, 0
	}
}

func (r *terminalRenderer) renderTableCells(w util.BufWriter, source []byte, node ast.Node, header bool) {
	cells := tableCells(node)
	lines := make([][]string, len(r.tableColWidths))
	rowHeight := 1
	for idx, columnWidth := range r.tableColWidths {
		text := ""
		if idx < len(cells) {
			text = getCellText(cells[idx], source)
		}
		_, contentWidth, _ := tableCellLayout(columnWidth)
		lines[idx] = strings.Split(ansi.Hardwrap(text, contentWidth, false), "\n")
		if len(lines[idx]) > rowHeight {
			rowHeight = len(lines[idx])
		}
	}

	for lineIdx := 0; lineIdx < rowHeight; lineIdx++ {
		_, _ = w.WriteString("│")
		for columnIdx, columnWidth := range r.tableColWidths {
			leftPadding, contentWidth, rightPadding := tableCellLayout(columnWidth)
			_, _ = w.WriteString(strings.Repeat(" ", leftPadding))
			text := ""
			if lineIdx < len(lines[columnIdx]) {
				text = lines[columnIdx][lineIdx]
			}
			if header && r.color && text != "" {
				_, _ = w.WriteString("\x1b[1m")
				_, _ = w.WriteString(text)
				_, _ = w.WriteString("\x1b[0m")
			} else {
				_, _ = w.WriteString(text)
			}
			padding := contentWidth - ansi.StringWidth(text) + rightPadding
			if padding > 0 {
				_, _ = w.WriteString(strings.Repeat(" ", padding))
			}
			_, _ = w.WriteString("│")
		}
		_, _ = w.WriteString("\n")
	}
}

func (r *terminalRenderer) renderTable(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.blockEnter(w, node)
		// Pre-compute column widths by walking the table AST once before
		// the renderer walks it for output. Add 2 for 1-space padding on
		// each side of the cell content.
		colWidths := make(map[int]int)
		ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			if n.Kind() != extast.KindTableCell {
				return ast.WalkContinue, nil
			}
			cell := n.(*extast.TableCell)
			idx := 0
			for prev := cell.PreviousSibling(); prev != nil; prev = prev.PreviousSibling() {
				if prev.Kind() == extast.KindTableCell {
					idx++
				}
			}
			text := getCellText(cell, source)
			cw := runewidth.StringWidth(text) + 2 // +2 for left/right padding
			if cw > colWidths[idx] {
				colWidths[idx] = cw
			}
			return ast.WalkContinue, nil
		})
		naturalWidths := make([]int, len(colWidths))
		for idx, cw := range colWidths {
			naturalWidths[idx] = cw
		}
		r.tableColWidths = fitTableColumnWidths(naturalWidths, r.width)
		drawTableBorder(w, r.tableColWidths, "┌", "┬", "┐")
	} else {
		drawTableBorder(w, r.tableColWidths, "└", "┴", "┘")
		r.tableColWidths = nil
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderTableHeader(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.renderTableCells(w, source, node, true)
		drawTableBorder(w, r.tableColWidths, "├", "┼", "┤")
	}
	return ast.WalkSkipChildren, nil
}

func (r *terminalRenderer) renderTableRow(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		r.renderTableCells(w, source, node, false)
	}
	return ast.WalkSkipChildren, nil
}

// RenderMarkdown renders markdown text to terminal-friendly output with ANSI
// color codes. It uses goldmark to parse the markdown and a custom renderer
// to produce terminal output.
func RenderMarkdown(text string) string {
	return renderMarkdown(text, useColor())
}

func renderMarkdown(text string, color bool) string {
	text = preprocessTableRows(text)

	buf := bytes.NewBuffer(nil)
	width := getTermWidth()
	if width > 1 {
		width--
	}
	tr := newTerminalRenderer(color, width)

	md := goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithRenderer(
			renderer.NewRenderer(
				renderer.WithNodeRenderers(
					util.Prioritized(tr, 100),
				),
			),
		),
	)

	if err := md.Convert([]byte(text), buf); err != nil {
		return HighlightCodeBlocks(text)
	}
	return buf.String()
}
