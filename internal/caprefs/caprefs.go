package caprefs

import (
	"regexp"
	"strings"
)

var dollarNameRe = regexp.MustCompile(`(^|[^A-Za-z0-9_])\$([A-Za-z_][A-Za-z0-9_.-]*)`)

func Names(text string) []string {
	matches := dollarNameRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := text[m[4]:m[5]]
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}
