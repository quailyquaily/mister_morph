package llm

import "strings"

// ShortModelName removes provider and gateway prefixes from a model name.
func ShortModelName(model string) string {
	model = strings.TrimSpace(model)
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx+1 < len(model) {
		model = model[idx+1:]
	}
	return strings.TrimSpace(model)
}
