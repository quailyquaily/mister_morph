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
	metadataMaxWidth := 0
	if animate {
		if terminalWidth, _, err := term.GetSize(int(file.Fd())); err == nil && terminalWidth > 4 {
			metadataMaxWidth = terminalWidth - 1
		}
	}
	metadataLine := renderChatSessionMetadataLine(provider, model, workspaceDir, version, metadataMaxWidth)
	frameLineCount := len(logoLines) + 2

	if !animate {
		_, _ = fmt.Fprintf(writer, "\n%s\n\n%s\n\n", chatBanner, metadataLine)
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
		content := ""
		if showMetadata {
			content = chatSecondaryStyle.Render(metadataLine)
		}
		_, _ = fmt.Fprintf(writer, "\r%s%s\n", ansi.EraseEntireLine, content)
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

func renderChatSessionMetadataLine(provider string, model string, workspaceDir string, version string, maximumWidth int) string {
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
		workspace = "workspace " + workspace
	}
	parts := make([]string, 0, 3)
	for _, part := range []string{session, workspace, version} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	line := "▓ " + strings.Join(parts, "  │  ")
	if maximumWidth > 0 {
		line = ansi.Truncate(line, maximumWidth, "…")
	}
	return line
}
