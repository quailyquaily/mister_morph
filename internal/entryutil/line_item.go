package entryutil

import (
	"regexp"
	"strings"
)

var metadataTuplePattern = regexp.MustCompile(`^\[([A-Za-z][A-Za-z0-9]*)\]\(([^()]+)\)$`)

func SplitMetadataAndContent(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, " | ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	meta := strings.TrimSpace(parts[0])
	content := strings.TrimSpace(parts[1])
	if meta == "" || content == "" {
		return "", "", false
	}
	return meta, content, true
}

func ParseMetadataTuples(raw string) (map[string]string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	parts := strings.Split(raw, ",")
	out := make(map[string]string, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			return nil, false
		}
		matches := metadataTuplePattern.FindStringSubmatch(item)
		if len(matches) != 3 {
			return nil, false
		}
		key := matches[1]
		val := strings.TrimSpace(matches[2])
		if val == "" {
			return nil, false
		}
		if _, exists := out[key]; exists {
			return nil, false
		}
		out[key] = val
	}
	return out, true
}

func FormatMetadataTuple(key string, value string) string {
	return "[" + strings.TrimSpace(key) + "](" + strings.TrimSpace(value) + ")"
}
