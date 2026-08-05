package codexauth

import "strings"

func UsesAPIKey(endpoint, apiKey string) bool {
	if strings.TrimSpace(apiKey) == "" {
		return false
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	defaultBase := strings.TrimRight(DefaultAPIBase, "/")
	return endpoint != "" &&
		!strings.EqualFold(endpoint, defaultBase) &&
		!strings.EqualFold(endpoint, defaultBase+"/v1")
}
