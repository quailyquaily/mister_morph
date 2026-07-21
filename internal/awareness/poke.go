package awareness

import (
	"mime"
	"strings"
)

type PokeInput struct {
	ContentType string
	BodyText    string
	Truncated   bool
	HasBody     bool
}

func (in PokeInput) Normalize() PokeInput {
	in.ContentType = normalizeContentType(in.ContentType)
	in.BodyText = strings.TrimSpace(in.BodyText)
	if !in.HasBody {
		in.ContentType = ""
		in.BodyText = ""
		in.Truncated = false
	}
	return in
}

func (in PokeInput) IsZero() bool {
	return !in.Normalize().HasBody
}

func (in PokeInput) MetaValue() map[string]any {
	in = in.Normalize()
	if in.IsZero() {
		return nil
	}
	out := map[string]any{"has_body": true}
	if in.ContentType != "" {
		out["content_type"] = in.ContentType
	}
	if in.BodyText != "" {
		out["body_text"] = in.BodyText
	}
	if in.Truncated {
		out["truncated"] = true
	}
	return out
}

func PokeInputFromMeta(meta map[string]any) (PokeInput, bool) {
	if len(meta) == 0 {
		return PokeInput{}, false
	}
	if input, ok := parsePokeInputValue(meta["poke"]); ok {
		return input, true
	}
	if rawAwareness, ok := meta["awareness"]; ok {
		if awareness, ok := rawAwareness.(map[string]any); ok {
			if input, ok := parsePokeInputValue(awareness["poke"]); ok {
				return input, true
			}
		}
	}
	rawHeartbeat, ok := meta["heartbeat"]
	if !ok {
		return PokeInput{}, false
	}
	heartbeat, ok := rawHeartbeat.(map[string]any)
	if !ok {
		return PokeInput{}, false
	}
	return parsePokeInputValue(heartbeat["poke"])
}

func normalizeContentType(raw string) string {
	contentType, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err == nil {
		return strings.TrimSpace(strings.ToLower(contentType))
	}
	return strings.TrimSpace(strings.ToLower(raw))
}

func parsePokeInputValue(value any) (PokeInput, bool) {
	switch typed := value.(type) {
	case nil:
		return PokeInput{}, false
	case PokeInput:
		typed = typed.Normalize()
		return typed, !typed.IsZero()
	case *PokeInput:
		if typed == nil {
			return PokeInput{}, false
		}
		normalized := typed.Normalize()
		return normalized, !normalized.IsZero()
	case map[string]any:
		input := PokeInput{
			ContentType: stringFromAny(typed["content_type"]),
			BodyText:    stringFromAny(typed["body_text"]),
			Truncated:   boolFromAny(typed["truncated"]),
			HasBody:     boolFromAny(typed["has_body"]),
		}
		if !input.HasBody && (input.BodyText != "" || input.ContentType != "" || input.Truncated) {
			input.HasBody = true
		}
		input = input.Normalize()
		return input, !input.IsZero()
	default:
		return PokeInput{}, false
	}
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func boolFromAny(value any) bool {
	flag, _ := value.(bool)
	return flag
}
