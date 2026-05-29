package llm

import (
	"strconv"
	"strings"
)

func ModelSupportsImageParts(model string) bool {
	model = normalizeModelName(model)
	if model == "" {
		return false
	}
	if matchModelFamily(model, "gpt-") {
		return true
	}
	if matchModelFamily(model, "gemini") {
		return true
	}
	if matchModelFamily(model, "gemma-4") {
		return true
	}
	if matchModelFamily(model, "kimi-") {
		return true
	}
	if matchModelFamily(model, "qwen3") {
		return true
	}
	if claude3OrAbove(model) {
		return true
	}
	if grok4OrAbove(model) {
		return true
	}
	return false
}

func ModelSupportsWebPTranscode(model string) bool {
	model = normalizeModelName(model)
	if model == "" {
		return false
	}
	if matchModelFamily(model, "gpt-") {
		return true
	}
	if matchModelFamily(model, "gemini") {
		return true
	}
	if matchModelFamily(model, "claude") {
		return true
	}
	return false
}

func claude3OrAbove(model string) bool {
	if major, ok := parseFamilyMajor(model, "claude"); ok && major >= 3 {
		return true
	}
	if sub, ok := modelAfterSlashFamily(model, "claude"); ok {
		if major, ok := parseFamilyMajor(sub, "claude"); ok && major >= 3 {
			return true
		}
	}
	return false
}

func grok4OrAbove(model string) bool {
	if major, ok := parseGrokMajor(model); ok && major >= 4 {
		return true
	}
	if sub, ok := modelAfterSlashFamily(model, "grok-"); ok {
		if major, ok := parseGrokMajor(sub); ok && major >= 4 {
			return true
		}
	}
	return false
}

func normalizeModelName(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func matchModelFamily(model string, family string) bool {
	if strings.HasPrefix(model, family) {
		return true
	}
	return strings.Contains(model, "/"+family)
}

func modelAfterSlashFamily(model string, family string) (string, bool) {
	idx := strings.Index(model, "/"+family)
	if idx < 0 || idx+1 >= len(model) {
		return "", false
	}
	return model[idx+1:], true
}

func parseFamilyMajor(model string, family string) (int, bool) {
	model = strings.TrimSpace(model)
	family = strings.TrimSpace(family)
	if family == "" || !strings.HasPrefix(model, family) {
		return 0, false
	}
	rest := model[len(family):]
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			continue
		}
		j := i
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		v, err := strconv.Atoi(rest[i:j])
		if err != nil {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

func parseGrokMajor(model string) (int, bool) {
	return parseFamilyMajor(strings.TrimSpace(model), "grok-")
}
