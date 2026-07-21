package textutil

import "strings"

// TruncateRunes trims surrounding whitespace and limits text by Unicode code points.
func TruncateRunes(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if maxChars <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars])
}
