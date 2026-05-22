package llmconfig

import "time"

type ClientConfig struct {
	Provider            string
	Endpoint            string
	APIKey              string
	Model               string
	ContextWindowTokens int64
	Headers             map[string]string
	RequestTimeout      time.Duration
}
