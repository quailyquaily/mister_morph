package codexauth

import "strings"

func UsesAPIKey(endpoint, apiKey string) bool {
	return strings.TrimSpace(endpoint) != "" && strings.TrimSpace(apiKey) != ""
}
