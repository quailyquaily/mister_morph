package guard

import (
	"regexp"
	"strings"
)

type Redactor struct {
	patterns []namedRe
}

type namedRe struct {
	name string
	re   *regexp.Regexp
}

func NewRedactor(cfg RedactionConfig) *Redactor {
	var patterns []namedRe

	// Built-ins (high-signal).
	patterns = append(patterns,
		mustNamed("private_key_block", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)),
		mustNamed("jwt_like", regexp.MustCompile(`(?m)\b[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)),
		mustNamed("bearer_line", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._-]{10,}\b`)),
		mustNamed("simple_kv", regexp.MustCompile(`(?i)\b([A-Za-z0-9_-]{1,32})(\s*[:=]\s*)([A-Za-z0-9._-]{12,})`)),
	)

	if cfg.Enabled {
		for _, p := range cfg.Patterns {
			if strings.TrimSpace(p.Re) == "" {
				continue
			}
			re, err := regexp.Compile(p.Re)
			if err != nil {
				continue
			}
			name := strings.TrimSpace(p.Name)
			if name == "" {
				name = "custom"
			}
			patterns = append(patterns, namedRe{name: name, re: re})
		}
	}

	return &Redactor{patterns: patterns}
}

func mustNamed(name string, re *regexp.Regexp) namedRe {
	return namedRe{name: name, re: re}
}

func (r *Redactor) RedactString(s string) (string, bool) {
	if strings.TrimSpace(s) == "" || r == nil || len(r.patterns) == 0 {
		return s, false
	}
	orig := s
	redacted := s

	redacted = r.replacePrivateKeyBlocks(redacted)
	redacted = r.replaceJWT(redacted)
	redacted = r.replaceBearer(redacted)
	redacted = r.replaceSensitiveKV(redacted)

	// Apply custom patterns last.
	for _, p := range r.patterns {
		switch p.name {
		case "private_key_block", "jwt_like", "bearer_line", "simple_kv":
			continue
		default:
			redacted = p.re.ReplaceAllString(redacted, "[redacted]")
		}
	}

	return redacted, redacted != orig
}

func (r *Redactor) replacePrivateKeyBlocks(s string) string {
	re := r.find("private_key_block")
	if re == nil {
		return s
	}
	return re.ReplaceAllString(s, "-----BEGIN PRIVATE KEY-----\n[redacted]\n-----END PRIVATE KEY-----")
}

func (r *Redactor) replaceJWT(s string) string {
	re := r.find("jwt_like")
	if re == nil {
		return s
	}
	return re.ReplaceAllString(s, "[redacted_jwt]")
}

func (r *Redactor) replaceBearer(s string) string {
	re := r.find("bearer_line")
	if re == nil {
		return s
	}
	return re.ReplaceAllString(s, "Bearer [redacted]")
}

func (r *Redactor) replaceSensitiveKV(s string) string {
	re := r.find("simple_kv")
	if re == nil {
		return s
	}
	return re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		key := sub[1]
		if !isSensitiveKeyLike(key) {
			return m
		}
		return key + sub[2] + "[redacted]"
	})
}

func (r *Redactor) find(name string) *regexp.Regexp {
	if r == nil {
		return nil
	}
	for _, p := range r.patterns {
		if p.name == name {
			return p.re
		}
	}
	return nil
}

func isSensitiveKeyLike(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	n := strings.ReplaceAll(strings.ReplaceAll(k, "-", ""), "_", "")
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
	}
	return false
}
