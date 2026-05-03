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
	return "\x1b[38;5;245m" + text + "\x1b[0m"
}

func Success(text string) string {
	return colorizeRGB(140, 170, 160, text)
}

func Warn(text string) string {
	return colorizeRGB(180, 160, 130, text)
}

func Dim(text string) string {
	return colorize("2", text)
}

func Key(text string) string {
	return colorizeRGB(150, 160, 210, text)
}

func colorize(code string, text string) string {
	if !useColor() {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func colorizeRGB(r, g, b int, text string) string {
	if !useColor() {
		return text
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, text)
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
