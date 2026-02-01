package agent

import (
	"encoding/json"
	"net/url"
	"strings"
)

type LogOptions struct {
	IncludeThoughts      bool
	IncludeToolParams    bool
	IncludeSkillContents bool

	MaxThoughtChars      int
	MaxJSONBytes         int
	MaxStringValueChars  int
	MaxSkillContentChars int

	RedactKeys []string
}

func DefaultLogOptions() LogOptions {
	return LogOptions{
		IncludeThoughts:      false,
		IncludeToolParams:    false,
		IncludeSkillContents: false,
		MaxThoughtChars:      2000,
		MaxJSONBytes:         32 * 1024,
		MaxStringValueChars:  2000,
		MaxSkillContentChars: 8000,
		RedactKeys: []string{
			"api_key",
			"apikey",
			"api-key",
			"authorization",
			"bearer",
			"cookie",
			"set-cookie",
			"password",
			"secret",
			"token",
			"access_token",
			"refresh_token",
		},
	}
}

func WithLogOptions(o LogOptions) Option {
	return func(e *Engine) {
		e.logOpts = normalizeLogOptions(o)
	}
}

func normalizeLogOptions(o LogOptions) LogOptions {
	d := DefaultLogOptions()

	if o.MaxThoughtChars <= 0 {
		o.MaxThoughtChars = d.MaxThoughtChars
	}
	if o.MaxJSONBytes <= 0 {
		o.MaxJSONBytes = d.MaxJSONBytes
	}
	if o.MaxStringValueChars <= 0 {
		o.MaxStringValueChars = d.MaxStringValueChars
	}
	if o.MaxSkillContentChars <= 0 {
		o.MaxSkillContentChars = d.MaxSkillContentChars
	}
	if len(o.RedactKeys) == 0 {
		o.RedactKeys = d.RedactKeys
	}
	return o
}

func truncateString(s string, max int) string {
	if max <= 0 {
		return s
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func shouldRedactKey(key string, redactKeys []string) bool {
	k := normalizeKeyForRedaction(key)
	if strings.TrimSpace(k) == "" {
		return false
	}
	for _, rk := range redactKeys {
		if normalizeKeyForRedaction(rk) == k {
			return true
		}
	}
	// Heuristic: redact common sensitive substrings.
	switch {
	case strings.Contains(k, "token"):
		return true
	case strings.Contains(k, "secret"):
		return true
	case strings.Contains(k, "password"):
		return true
	case strings.Contains(k, "apikey"):
		return true
	case strings.Contains(k, "authorization"):
		return true
	case strings.Contains(k, "cookie"):
		return true
	}
	return false
}

func normalizeKeyForRedaction(key string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, "-", "")
	k = strings.ReplaceAll(k, "_", "")
	return k
}

func sanitizeValue(v any, maxStr int, redactKeys []string, keyHint string) any {
	if keyHint != "" && shouldRedactKey(keyHint, redactKeys) {
		return "[redacted]"
	}

	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = sanitizeValue(vv, maxStr, redactKeys, k)
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, vv := range x {
			out = append(out, sanitizeValue(vv, maxStr, redactKeys, ""))
		}
		return out
	case string:
		return truncateString(x, maxStr)
	default:
		return v
	}
}

func sanitizeParams(params map[string]any, maxStr int, redactKeys []string) map[string]any {
	if params == nil {
		return nil
	}
	return sanitizeValue(params, maxStr, redactKeys, "").(map[string]any)
}

func paramsAsJSON(params map[string]any, maxJSONBytes int, maxStr int, redactKeys []string) string {
	safe := sanitizeParams(params, maxStr, redactKeys)
	b, err := json.Marshal(safe)
	if err != nil {
		return ""
	}
	if maxJSONBytes > 0 && len(b) > maxJSONBytes {
		b = b[:maxJSONBytes]
		return string(b) + "...(truncated)"
	}
	return string(b)
}

func sanitizeURLForLog(raw string, opts LogOptions) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return truncateString(raw, opts.MaxStringValueChars)
	}
	q := u.Query()
	changed := false
	for k := range q {
		if shouldRedactKey(k, opts.RedactKeys) {
			q.Set(k, "[redacted]")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return truncateString(u.String(), opts.MaxStringValueChars)
}
