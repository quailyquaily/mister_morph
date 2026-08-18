package outputfmt

import (
	"net/url"
	"regexp"
	"strings"
)

var absoluteURLInTextRE = regexp.MustCompile(`https?://[^\s"'<>]+`)

// FormatErrorForDisplay sanitizes error text for user-facing channels.
// It removes URL hosts and keeps only path/query/fragment parts.
func FormatErrorForDisplay(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeErrorText(err.Error())
}

// SanitizeErrorText removes URL hosts from arbitrary text while keeping
// path/query/fragment details.
func SanitizeErrorText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return absoluteURLInTextRE.ReplaceAllStringFunc(raw, sanitizeURLInText)
}

func sanitizeURLInText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return raw
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if q := redactSensitiveQuery(u.Query()); q != "" {
		path += "?" + q
	}
	if frag := strings.TrimSpace(u.EscapedFragment()); frag != "" {
		path += "#" + frag
	}
	return path
}

func redactSensitiveQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	for k := range q {
		if isSensitiveQueryKey(k) {
			q.Set(k, "[redacted]")
		}
	}
	return q.Encode()
}

func isSensitiveQueryKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	n := strings.ReplaceAll(strings.ReplaceAll(k, "-", ""), "_", "")
	if n == "key" {
		return true
	}
	switch {
	case strings.Contains(n, "apikey"):
		return true
	case strings.Contains(n, "authorization"):
		return true
	case strings.Contains(n, "token"):
		return true
	case strings.Contains(n, "secret"):
		return true
	case strings.Contains(n, "password"):
		return true
	case strings.Contains(n, "cookie"):
		return true
	}
	return false
}
