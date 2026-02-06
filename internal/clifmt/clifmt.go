package clifmt

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

func Headerf(format string, args ...any) string {
	text := fmt.Sprintf(format, args...)
	if !useColor() {
		return text
	}
	return "\x1b[1;36m" + text + "\x1b[0m"
}

func Success(text string) string {
	return colorize("32", text)
}

func Warn(text string) string {
	return colorize("33", text)
}

func Dim(text string) string {
	return colorize("2", text)
}

func Key(text string) string {
	return colorize("1;33", text)
}

func colorize(code string, text string) string {
	if !useColor() {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}
