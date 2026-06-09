package shellenv

import (
	"fmt"
	"os"
	"strings"
)

type InjectedEnvVar struct {
	Name  string
	Value string
}

func NormalizeName(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ""
	}
	for i, r := range key {
		switch {
		case r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			if i == 0 {
				continue
			}
		case i > 0 && r >= '0' && r <= '9':
			continue
		default:
			return ""
		}
	}
	return key
}

func ParseInjectedEnvVars(raw any) ([]InjectedEnvVar, error) {
	if raw == nil {
		return nil, nil
	}
	items, err := asAnySlice(raw)
	if err != nil {
		return nil, err
	}
	out := make([]InjectedEnvVar, 0, len(items))
	for i, item := range items {
		entry, err := parseInjectedEnvVarItem(item, i)
		if err != nil {
			return nil, err
		}
		if entry.Name == "" {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func CloneInjectedEnvVars(in []InjectedEnvVar) []InjectedEnvVar {
	if len(in) == 0 {
		return nil
	}
	out := make([]InjectedEnvVar, len(in))
	copy(out, in)
	return out
}

func InjectedEnvVarsFromConfig(raw any) []InjectedEnvVar {
	out, _ := ParseInjectedEnvVars(raw)
	return out
}

func parseInjectedEnvVarItem(item any, index int) (InjectedEnvVar, error) {
	switch value := item.(type) {
	case string:
		return resolveInjectedEnvVarName(NormalizeName(value))
	case map[string]any:
		return parseInjectedEnvVarMap(value)
	case map[string]string:
		return parseInjectedEnvVarMap(normalizeStringStringMap(value))
	case map[any]any:
		return parseInjectedEnvVarMap(normalizeStringAnyMap(value))
	default:
		return InjectedEnvVar{}, fmt.Errorf("injected_env_vars[%d] must be a string or object", index)
	}
}

func parseInjectedEnvVarMap(raw map[string]any) (InjectedEnvVar, error) {
	name := NormalizeName(stringValue(raw["name"]))
	if name == "" {
		return InjectedEnvVar{}, nil
	}
	if _, ok := raw["value"]; ok {
		return InjectedEnvVar{Name: name, Value: stringValue(raw["value"])}, nil
	}
	return resolveInjectedEnvVarName(name)
}

func resolveInjectedEnvVarName(name string) (InjectedEnvVar, error) {
	if name == "" {
		return InjectedEnvVar{}, nil
	}
	value, ok := lookupParentEnvValue(name)
	if !ok {
		return InjectedEnvVar{}, nil
	}
	return InjectedEnvVar{Name: name, Value: value}, nil
}

func lookupParentEnvValue(name string) (string, bool) {
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func asAnySlice(raw any) ([]any, error) {
	switch value := raw.(type) {
	case []any:
		return value, nil
	case []string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out, nil
	case []map[string]any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out, nil
	case []map[any]any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out, nil
	case []map[string]string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out, nil
	default:
		return nil, fmt.Errorf("injected_env_vars must be a list")
	}
}

func normalizeStringAnyMap(in map[any]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		key, ok := k.(string)
		if !ok {
			continue
		}
		out[key] = v
	}
	return out
}

func normalizeStringStringMap(in map[string]string) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringValue(raw any) string {
	switch value := raw.(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		return fmt.Sprint(value)
	}
}
