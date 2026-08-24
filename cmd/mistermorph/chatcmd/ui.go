package chatcmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

const chatBanner = `▄▄   ▄▄  ▄▄▄  ▄▄▄▄  ▄▄▄▄  ▄▄ ▄▄
██▀▄▀██ ██▀██ ██▄█▄ ██▄█▀ ██▄██
██   ██ ▀███▀ ██ ██ ██    ██ ██`

var chatBootLogoStyle = lipgloss.NewStyle().Bold(true)

func printChatSessionHeader(writer io.Writer, compact bool, provider string, model string, workspaceDir string, version string) {
	if compact {
		return
	}
	logoLines := strings.Split(chatBanner, "\n")
	logoWidth := 0
	for _, line := range logoLines {
		logoWidth = max(logoWidth, ansi.StringWidth(line))
	}

	file, animate := writer.(*os.File)
	animate = animate && term.IsTerminal(int(file.Fd()))
	panelMaxWidth := 0
	if animate {
		if terminalWidth, _, err := term.GetSize(int(file.Fd())); err == nil && terminalWidth > 4 {
			panelMaxWidth = terminalWidth - 1
		}
	}
	panelLines := renderChatSessionMetadataPanel(provider, model, workspaceDir, version, logoWidth, panelMaxWidth)
	frameLineCount := len(logoLines) + 1 + len(panelLines)

	if !animate {
		_, _ = fmt.Fprintf(writer, "\n%s\n\n%s\n\n", chatBanner, strings.Join(panelLines, "\n"))
		return
	}

	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprint(writer, ansi.HideCursor)
	drawn := false
	drawFrame := func(visibleColumns int, showMetadata bool) {
		if drawn {
			_, _ = fmt.Fprint(writer, ansi.CursorUp(frameLineCount))
		}
		for _, line := range logoLines {
			runes := []rune(line)
			end := min(visibleColumns, len(runes))
			_, _ = fmt.Fprintf(writer, "\r%s%s\n", ansi.EraseEntireLine, chatBootLogoStyle.Render(string(runes[:end])))
		}
		_, _ = fmt.Fprintf(writer, "\r%s\n", ansi.EraseEntireLine)
		for index, line := range panelLines {
			content := ""
			if showMetadata {
				style := chatSecondaryStyle
				if index == 0 || index == len(panelLines)-1 {
					style = chatMutedStyle
				}
				content = style.Render(line)
			}
			_, _ = fmt.Fprintf(writer, "\r%s%s\n", ansi.EraseEntireLine, content)
		}
		drawn = true
	}

	for visibleColumns := 0; visibleColumns < logoWidth; visibleColumns += 4 {
		drawFrame(visibleColumns, false)
		time.Sleep(28 * time.Millisecond)
	}
	drawFrame(logoWidth, false)
	drawFrame(logoWidth, true)
	time.Sleep(100 * time.Millisecond)
	drawFrame(logoWidth, false)
	time.Sleep(70 * time.Millisecond)
	drawFrame(logoWidth, true)
	_, _ = fmt.Fprint(writer, ansi.ShowCursor)
	_, _ = fmt.Fprintln(writer)
}

func renderChatSessionMetadataPanel(provider string, model string, workspaceDir string, version string, minimumWidth int, maximumWidth int) []string {
	sessionParts := make([]string, 0, 2)
	if provider = strings.TrimSpace(provider); provider != "" {
		sessionParts = append(sessionParts, provider)
	}
	if model = strings.TrimSpace(model); model != "" {
		sessionParts = append(sessionParts, model)
	}
	session := strings.Join(sessionParts, " / ")
	version = strings.TrimSpace(version)
	if version != "" {
		version = "version " + version
	}
	workspace := workspaceDisplayName(workspaceDir)
	if workspace != "" {
		workspace = "workspace  " + workspace
	}
	if session == "" && version == "" && workspace == "" {
		return nil
	}

	firstRowWidth := ansi.StringWidth(session) + ansi.StringWidth(version)
	if session != "" && version != "" {
		firstRowWidth += 3
	}
	panelWidth := max(minimumWidth, max(firstRowWidth, ansi.StringWidth(workspace))+4)
	if maximumWidth >= 4 {
		panelWidth = min(panelWidth, maximumWidth)
	}
	panelWidth = max(4, panelWidth)
	contentWidth := panelWidth - 4

	pad := func(line string) string {
		line = ansi.Truncate(line, contentWidth, "…")
		return line + strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(line)))
	}
	align := func(left string, right string) string {
		if right == "" {
			return pad(left)
		}
		right = ansi.Truncate(right, contentWidth, "…")
		rightWidth := ansi.StringWidth(right)
		availableLeft := contentWidth - rightWidth - 3
		if left == "" || availableLeft <= 0 {
			return strings.Repeat(" ", max(0, contentWidth-rightWidth)) + right
		}
		left = ansi.Truncate(left, availableLeft, "…")
		gap := contentWidth - ansi.StringWidth(left) - rightWidth
		return left + strings.Repeat(" ", gap) + right
	}

	rows := []string{align(session, version)}
	if workspace != "" {
		rows = append(rows, pad(workspace))
	}
	border := strings.Repeat("━", panelWidth-2)
	lines := make([]string, 0, len(rows)+2)
	lines = append(lines, "┏"+border+"┓")
	for _, row := range rows {
		lines = append(lines, "┃ "+row+" ┃")
	}
	lines = append(lines, "┗"+border+"┛")
	return lines
}
