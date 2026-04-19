package chatcmd

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const chatBanner = `

▄▄   ▄▄  ▄▄▄  ▄▄▄▄  ▄▄▄▄  ▄▄ ▄▄
██▀▄▀██ ██▀██ ██▄█▄ ██▄█▀ ██▄██
██   ██ ▀███▀ ██ ██ ██    ██ ██
`

func buildUserName() string {
	userName := ""
	if u, err := user.Current(); err == nil && u != nil {
		userName = strings.TrimSpace(u.Username)
	}
	if userName == "" {
		userName = strings.TrimSpace(os.Getenv("USER"))
	}
	if userName == "" {
		userName = "you"
	}
	return userName
}

func buildUserPrompt(compactMode bool, userName string) string {
	if compactMode {
		return "\033[32m• \033[0m"
	}
	return fmt.Sprintf("\033[42m\033[30m %s> \033[0m ", userName)
}

func thinkingAnimation(writer io.Writer) (stop func(), setMessage func(msg string)) {
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	ticker := time.NewTicker(80 * time.Millisecond)
	done := make(chan struct{})
	msgMu := sync.RWMutex{}
	msg := "assistant is thinking..."
	var wg sync.WaitGroup

	var lastLinesMu sync.Mutex
	lastLines := 1

	calcLines := func(text string) int {
		width, _, _ := term.GetSize(int(os.Stdout.Fd()))
		if width <= 0 {
			width = 80
		}
		prefixWidth := 2 // spinner icon (1) + space (1)
		totalWidth := prefixWidth + stringDisplayWidth(text)
		lines := totalWidth / width
		if totalWidth%width != 0 {
			lines++
		}
		if lines < 1 {
			lines = 1
		}
		return lines
	}

	buildClearSeq := func(n int) string {
		if n <= 1 {
			return "\r\033[K"
		}
		var b strings.Builder
		for i := 1; i < n; i++ {
			b.WriteString("\033[A")
		}
		b.WriteString("\r")
		for i := 0; i < n; i++ {
			b.WriteString("\033[2K")
			if i < n-1 {
				b.WriteString("\033[B")
			}
		}
		for i := 1; i < n; i++ {
			b.WriteString("\033[A")
		}
		b.WriteString("\r")
		return b.String()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-ticker.C:
				msgMu.RLock()
				currentMsg := msg
				msgMu.RUnlock()

				lastLinesMu.Lock()
				prevLines := lastLines
				lastLines = calcLines(currentMsg)
				lastLinesMu.Unlock()

				clearSeq := buildClearSeq(prevLines)
				_, _ = fmt.Fprintf(writer, "%s\033[36m%s\033[0m \033[90m%s\033[0m", clearSeq, spinner[i%len(spinner)], currentMsg)
				i++
			case <-done:
				return
			}
		}
	}()
	stop = func() {
		close(done)
		ticker.Stop()
		wg.Wait()

		lastLinesMu.Lock()
		prevLines := lastLines
		lastLinesMu.Unlock()

		_, _ = fmt.Fprint(writer, buildClearSeq(prevLines))
	}
	setMessage = func(newMsg string) {
		msgMu.Lock()
		msg = truncateString(newMsg, 80)
		msgMu.Unlock()
	}
	return stop, setMessage
}

func printChatSessionHeader(writer io.Writer, model string, fileCacheDir string) {
	_, _ = fmt.Fprint(writer, chatBanner)
	if model != "" {
		_, _ = fmt.Fprintf(writer, "model=%s\n", model)
	}
	if fileCacheDir != "" {
		_, _ = fmt.Fprintf(writer, "file_cache_dir=%s\n", fileCacheDir)
	}
	_, _ = fmt.Fprintln(writer, "\033[90mInteractive chat started. Press Ctrl+C or type /exit to quit.\033[0m")
}
