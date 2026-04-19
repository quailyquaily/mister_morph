package chatcmd

import (
	"fmt"
	"io"
)

func handleExit(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, "\nBye! 👋")
}

func handleHelp(writer io.Writer) {
	_, _ = fmt.Fprint(writer, "\n\033[1m\033[36m=== MisterMorph Chat Commands ===\033[0m\n\n")
	_, _ = fmt.Fprintln(writer, "\033[33mGeneral\033[0m")
	_, _ = fmt.Fprintln(writer, "  /exit, /quit          Exit the chat session")
	_, _ = fmt.Fprintln(writer, "  /help                 Show this help message")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "\033[33mProject Memory\033[0m")
	_, _ = fmt.Fprintln(writer, "  /remember <content>   Add an entry to project memory")
	_, _ = fmt.Fprintln(writer, "  /memory               View current project memory")
	_, _ = fmt.Fprintln(writer, "  /forget               Clear all project memory")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "\033[33mProject Context\033[0m")
	_, _ = fmt.Fprintln(writer, "  /init                 Read AGENTS.md from current directory")
	_, _ = fmt.Fprintln(writer, "  /update               Regenerate AGENTS.md via AI")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "\033[33mModel\033[0m")
	_, _ = fmt.Fprintln(writer, "  /model                Show current model selection state")
	_, _ = fmt.Fprintln(writer, "  /model list           List all available LLM profiles")
	_, _ = fmt.Fprintln(writer, "  /model set <profile>  Switch to specified profile")
	_, _ = fmt.Fprintln(writer, "  /model reset          Reset to automatic route selection")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "\033[33mShortcuts\033[0m")
	_, _ = fmt.Fprintln(writer, "  Tab                   Command auto-completion")
	_, _ = fmt.Fprintln(writer, "  Ctrl+C                Interrupt current turn / clear input line")
	_, _ = fmt.Fprintln(writer, "  ↑ / ↓                 Browse input history")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "\033[90mTip: Type any text to chat with the assistant.\033[0m")
}
