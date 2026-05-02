package clifmt

import (
	"bytes"
	"fmt"
	"strings"

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
	styleStack []string
}

func newTerminalRenderer() *terminalRenderer {
	return &terminalRenderer{}
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
	if !useColor() {
		return
	}
	styles := r.currentStyles()
	if styles != "" {
		w.WriteString(styles)
	}
}

func (r *terminalRenderer) closeStyle(w util.BufWriter) {
	if !useColor() {
		return
	}
	r.popStyle()
	w.WriteString("\x1b[0m")
	r.applyStyles(w)
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
	reg.Register(extast.KindTableCell, r.renderTableCell)
}

func (r *terminalRenderer) renderDocument(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)
	if entering {
		if useColor() {
			r.pushStyle("\x1b[1m")
			_, _ = w.WriteString("\x1b[1m")
		}
		for i := 0; i < n.Level; i++ {
			_, _ = w.WriteString("#")
		}
		_, _ = w.WriteString(" ")
	} else {
		if useColor() {
			r.closeStyle(w)
		}
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderParagraph(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		if node.Parent() != nil && node.Parent().Kind() == ast.KindListItem {
			return ast.WalkContinue, nil
		}
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Text)
	_, _ = w.Write(n.Segment.Value(source))
	if n.HardLineBreak() {
		_, _ = w.WriteString("\n")
	} else if n.SoftLineBreak() {
		_, _ = w.WriteString(" ")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderEmphasis(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !useColor() {
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
	n := node.(*ast.FencedCodeBlock)

	lang := ""
	if n.Info != nil {
		lang = strings.TrimSpace(string(n.Info.Text(source)))
	}

	var codeBuf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = codeBuf.Write(line.Value(source))
	}
	code := codeBuf.String()

	highlighted, err := highlightCode(code, lang)
	if err != nil {
		_, _ = w.WriteString(code)
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString("\n")
	_, _ = w.WriteString(wrapInBox(highlighted, lang))
	_, _ = w.WriteString("\n")
	return ast.WalkSkipChildren, nil
}

func (r *terminalRenderer) renderCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.CodeBlock)

	var codeBuf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		_, _ = codeBuf.Write(line.Value(source))
	}
	code := codeBuf.String()

	highlighted, err := highlightCode(code, "")
	if err != nil {
		_, _ = w.WriteString(code)
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString("\n")
	_, _ = w.WriteString(wrapInBox(highlighted, ""))
	_, _ = w.WriteString("\n")
	return ast.WalkSkipChildren, nil
}

func (r *terminalRenderer) renderCodeSpan(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !useColor() {
		return ast.WalkContinue, nil
	}
	if entering {
		r.pushStyle("\x1b[90m")
		_, _ = w.WriteString("\x1b[90m")
	} else {
		r.closeStyle(w)
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderListItem(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
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
		if useColor() {
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
		_, _ = w.WriteString("\x1b[38;5;245m│ \x1b[0m")
	} else {
		_, _ = w.WriteString("\n")
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
	_, _ = w.WriteString("\n")
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderTable(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderTableHeader(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		if useColor() {
			r.pushStyle("\x1b[1m")
			_, _ = w.WriteString("\x1b[1m")
		}
		return ast.WalkContinue, nil
	}
	// Exiting header: newline, then separator line.
	_, _ = w.WriteString("\n")
	if useColor() {
		r.closeStyle(w)
	}
	if table, ok := node.Parent().(*extast.Table); ok && len(table.Alignments) > 0 {
		cols := len(table.Alignments)
		for i := 0; i < cols; i++ {
			if i > 0 {
				_, _ = w.WriteString(" | ")
			}
			_, _ = w.WriteString("---")
		}
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderTableRow(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("\n")
	}
	return ast.WalkContinue, nil
}

func (r *terminalRenderer) renderTableCell(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	if node.PreviousSibling() != nil {
		_, _ = w.WriteString(" | ")
	}
	return ast.WalkContinue, nil
}

// RenderMarkdown renders markdown text to terminal-friendly output with ANSI
// color codes. It uses goldmark to parse the markdown and a custom renderer
// to produce terminal output.
func RenderMarkdown(text string) string {
	return renderMarkdown(text, useColor())
}

func renderMarkdown(text string, color bool) string {
	if !color {
		return HighlightCodeBlocks(text)
	}

	buf := bytes.NewBuffer(nil)
	tr := newTerminalRenderer()

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
