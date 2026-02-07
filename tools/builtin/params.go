package builtin

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func parseIntDefault(raw any, fallback int) int {
	switch v := raw.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return fallback
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return fallback
		}
		return n
	default:
		return fallback
	}
}

func parseBoolDefault(raw any, fallback bool) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		text := strings.TrimSpace(strings.ToLower(v))
		if text == "" {
			return fallback
		}
		if text == "1" || text == "true" || text == "yes" || text == "y" {
			return true
		}
		if text == "0" || text == "false" || text == "no" || text == "n" {
			return false
		}
		return fallback
	default:
		return fallback
	}
}

func parseFloatDefault(raw any, fallback float64) float64 {
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return fallback
		}
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return fallback
		}
		return n
	default:
		return fallback
	}
}

func parseDuration(raw any, fallback time.Duration) (time.Duration, error) {
	switch v := raw.(type) {
	case nil:
		return fallback, nil
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return fallback, nil
		}
		d, err := time.ParseDuration(text)
		if err != nil {
			return 0, err
		}
		return d, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("invalid duration number")
		}
		return time.Duration(v) * time.Second, nil
	case int:
		return time.Duration(v) * time.Second, nil
	case int64:
		return time.Duration(v) * time.Second, nil
	default:
		return fallback, nil
	}
}

func parseStringSlice(raw any) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(item)
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, _ := item.(string)
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func asInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		if x > math.MaxInt64 {
			return 0, false
		}
		return int64(x), true
	case float32:
		return int64(x), true
	case float64:
		return int64(x), true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}
