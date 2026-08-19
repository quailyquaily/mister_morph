package agent

import (
	"encoding/json"
	"strings"
)

const maxInjectedMetaBytes = 4 * 1024

func buildInjectedMetaMessage(meta map[string]any) (string, bool) {
	if len(meta) == 0 {
		return "", false
	}

	envelope := map[string]any{"mister_morph_meta": meta}
	b, err := json.Marshal(envelope)
	if err == nil && len(b) <= maxInjectedMetaBytes {
		return string(b), true
	}

	// Truncate best-effort by keeping only essential keys.
	stub := map[string]any{
		"truncated": true,
	}
	for _, key := range []string{
		"trigger",
		"model",
		"correlation_id",
		"run_id",
		"task_id",
		"trace_id",
		"topic_id",
		"origin_event_id",
	} {
		copyMetaString(stub, meta, key)
	}
	b, _ = json.Marshal(map[string]any{"mister_morph_meta": stub})
	if len(b) <= maxInjectedMetaBytes {
		return string(b), true
	}

	// Final fallback: smallest possible stub.
	return `{"mister_morph_meta":{"truncated":true}}`, true
}

func copyMetaString(dst map[string]any, src map[string]any, key string) {
	v, ok := src[key]
	if !ok {
		return
	}
	s, ok := v.(string)
	if !ok {
		return
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	dst[key] = s
}
