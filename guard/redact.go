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

const (
	redactionReasonPrivateKey    = "redacted_private_key_block"
	redactionReasonJWT           = "redacted_jwt"
	redactionReasonBearerToken   = "redacted_bearer_token"
	redactionReasonMMEnv         = "redacted_mister_morph_env"
	redactionReasonSecretKV      = "redacted_secret_value"
	redactionReasonCustomPattern = "redacted_custom_pattern"
)

func NewRedactor(cfg RedactionConfig) *Redactor {
	var patterns []namedRe

	// Built-ins (high-signal).
	patterns = append(patterns,
		namedRe{name: "private_key_block", re: regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)},
		namedRe{name: "jwt_like", re: regexp.MustCompile(`(?m)\b[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
		namedRe{name: "bearer_line", re: regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._-]{10,}\b`)},
		namedRe{name: "mister_morph_env_kv", re: regexp.MustCompile(`\b(MISTER_MORPH_[A-Za-z0-9_]{1,64})(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s]+)`)},
		namedRe{name: "mister_morph_env_name", re: regexp.MustCompile(`\bMISTER_MORPH_[A-Za-z0-9_]{1,64}\b`)},
		namedRe{name: "simple_kv", re: regexp.MustCompile(`(?i)\b([A-Za-z0-9_-]{1,32})(\s*[:=]\s*)([A-Za-z0-9._-]{12,})`)},
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

func (r *Redactor) RedactString(s string) (string, bool) {
	redacted, changed, _ := r.RedactStringDetailed(s)
	return redacted, changed
}

func (r *Redactor) RedactStringDetailed(s string) (string, bool, []string) {
	if strings.TrimSpace(s) == "" || r == nil || len(r.patterns) == 0 {
		return s, false, nil
	}
	orig := s
	redacted := s
	reasons := make([]string, 0, 4)
	seenReasons := map[string]struct{}{}

	addReason := func(reason string) {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return
		}
		if _, ok := seenReasons[reason]; ok {
			return
		}
		seenReasons[reason] = struct{}{}
		reasons = append(reasons, reason)
	}

	var changed bool
	redacted, changed = r.replacePrivateKeyBlocks(redacted)
	if changed {
		addReason(redactionReasonPrivateKey)
	}
	redacted, changed = r.replaceJWT(redacted)
	if changed {
		addReason(redactionReasonJWT)
	}
	redacted, changed = r.replaceBearer(redacted)
	if changed {
		addReason(redactionReasonBearerToken)
	}
	redacted, changed = r.replaceMisterMorphEnvKV(redacted)
	if changed {
		addReason(redactionReasonMMEnv)
	}
	redacted, changed = r.replaceSensitiveKV(redacted)
	if changed {
		addReason(redactionReasonSecretKV)
	}
	redacted, changed = r.replaceMisterMorphEnvNames(redacted)
	if changed {
		addReason(redactionReasonMMEnv)
	}

	// Apply custom patterns last.
	for _, p := range r.patterns {
		switch p.name {
		case "private_key_block", "jwt_like", "bearer_line", "mister_morph_env_kv", "mister_morph_env_name", "simple_kv":
			continue
		default:
			next := p.re.ReplaceAllString(redacted, "[redacted]")
			if next != redacted {
				redacted = next
				addReason(customRedactionReason(p.name))
			}
		}
	}

	return redacted, redacted != orig, reasons
}

func (r *Redactor) redactValueDetailed(value any) (any, bool, []string) {
	switch value := value.(type) {
	case string:
		return r.RedactStringDetailed(value)
	case map[string]any:
		redacted := make(map[string]any, len(value))
		changed := false
		var reasons []string
		for key, item := range value {
			redactedKey, keyChanged, keyReasons := r.RedactStringDetailed(key)
			redactedItem, itemChanged, itemReasons := r.redactValueDetailed(item)
			redacted[redactedKey] = redactedItem
			changed = changed || keyChanged || itemChanged
			reasons = appendRedactionReasons(reasons, keyReasons)
			reasons = appendRedactionReasons(reasons, itemReasons)
		}
		return redacted, changed, reasons
	case []any:
		redacted := make([]any, len(value))
		changed := false
		var reasons []string
		for index, item := range value {
			redactedItem, itemChanged, itemReasons := r.redactValueDetailed(item)
			redacted[index] = redactedItem
			changed = changed || itemChanged
			reasons = appendRedactionReasons(reasons, itemReasons)
		}
		return redacted, changed, reasons
	default:
		return value, false, nil
	}
}

func appendRedactionReasons(dst, src []string) []string {
	for _, candidate := range src {
		found := false
		for _, existing := range dst {
			if existing == candidate {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, candidate)
		}
	}
	return dst
}

func (r *Redactor) replacePrivateKeyBlocks(s string) (string, bool) {
	re := r.find("private_key_block")
	if re == nil {
		return s, false
	}
	next := re.ReplaceAllString(s, "-----BEGIN PRIVATE KEY-----\n[redacted]\n-----END PRIVATE KEY-----")
	return next, next != s
}

func (r *Redactor) replaceJWT(s string) (string, bool) {
	re := r.find("jwt_like")
	if re == nil {
		return s, false
	}
	next := re.ReplaceAllString(s, "[redacted_jwt]")
	return next, next != s
}

func (r *Redactor) replaceBearer(s string) (string, bool) {
	re := r.find("bearer_line")
	if re == nil {
		return s, false
	}
	next := re.ReplaceAllString(s, "Bearer [redacted]")
	return next, next != s
}

func (r *Redactor) replaceMisterMorphEnvKV(s string) (string, bool) {
	re := r.find("mister_morph_env_kv")
	if re == nil {
		return s, false
	}
	next := re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		return "[redacted_env]" + sub[2] + "[redacted]"
	})
	return next, next != s
}

func (r *Redactor) replaceSensitiveKV(s string) (string, bool) {
	re := r.find("simple_kv")
	if re == nil {
		return s, false
	}
	next := re.ReplaceAllStringFunc(s, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		key := sub[1]
		if !IsSensitiveKey(key) {
			return m
		}
		return key + sub[2] + "[redacted]"
	})
	return next, next != s
}

func (r *Redactor) replaceMisterMorphEnvNames(s string) (string, bool) {
	re := r.find("mister_morph_env_name")
	if re == nil {
		return s, false
	}
	next := re.ReplaceAllString(s, "[redacted_env]")
	return next, next != s
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

// IsSensitiveKey reports whether a key names credential-like content handled by Redactor.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	n := strings.ReplaceAll(strings.ReplaceAll(k, "-", ""), "_", "")
	return strings.Contains(n, "apikey") ||
		strings.Contains(n, "authorization") ||
		strings.Contains(n, "token") ||
		strings.Contains(n, "secret") ||
		strings.Contains(n, "password")
}

func customRedactionReason(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	if slug == "" {
		return redactionReasonCustomPattern
	}
	var b strings.Builder
	prevUnderscore := false
	for _, ch := range slug {
		switch {
		case (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9'):
			b.WriteRune(ch)
			prevUnderscore = false
		default:
			if prevUnderscore {
				continue
			}
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	slug = strings.Trim(b.String(), "_")
	if slug == "" {
		return redactionReasonCustomPattern
	}
	return redactionReasonCustomPattern + "_" + slug
}
