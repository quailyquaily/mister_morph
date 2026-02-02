package strutil

import "unicode/utf8"

// TruncateUTF8 returns the longest prefix of s that is at most maxBytes
// bytes and does not split a multi-byte UTF-8 character.
func TruncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
