package chatcmd

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

func printChatSessionHeader(writer io.Writer, compact bool, model string, workspaceDir string) {
	if compact {
		return
	}
	parts := []string{"MisterMorph"}
	if model = strings.TrimSpace(model); model != "" {
		parts = append(parts, model)
	}
	if workspaceDir = strings.TrimSpace(workspaceDir); workspaceDir != "" {
		parts = append(parts, filepath.Base(filepath.Clean(workspaceDir)))
	}
	_, _ = fmt.Fprintln(writer, strings.Join(parts, " · "))
}
