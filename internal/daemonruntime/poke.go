package daemonruntime

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"
)

const pokeBodyPreviewLimit = 4 * 1024

type PokeInput struct {
	ContentType string
	BodyText    string
	Truncated   bool
	HasBody     bool
}

func (in PokeInput) Normalize() PokeInput {
	in.ContentType = normalizePokeContentType(in.ContentType)
	in.BodyText = strings.TrimSpace(in.BodyText)
	if !in.HasBody {
		in.ContentType = ""
		in.BodyText = ""
		in.Truncated = false
	}
	return in
}

func (in PokeInput) IsZero() bool {
	in = in.Normalize()
	return !in.HasBody
}

func (in PokeInput) MetaValue() map[string]any {
	in = in.Normalize()
	if in.IsZero() {
		return nil
	}
	out := map[string]any{
		"has_body": true,
	}
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
	rawAwareness, ok := meta["awareness"]
	if ok {
		awareness, ok := rawAwareness.(map[string]any)
		if ok {
			if input, ok := parsePokeInputValue(awareness["poke"]); ok {
				return input, true
			}
		}
	}
	rawHeartbeat, ok := meta["heartbeat"]
	if !ok {
		return PokeInput{}, false
	}
	hb, ok := rawHeartbeat.(map[string]any)
	if !ok {
		return PokeInput{}, false
	}
	return parsePokeInputValue(hb["poke"])
}

func readPokeInput(r *http.Request) (PokeInput, error) {
	if r == nil || r.Body == nil {
		return PokeInput{}, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, pokeBodyPreviewLimit+1))
	if err != nil {
		return PokeInput{}, err
	}
	if len(raw) == 0 {
		return PokeInput{}, nil
	}
	input := PokeInput{
		ContentType: normalizePokeContentType(r.Header.Get("Content-Type")),
		HasBody:     true,
	}
	if len(raw) > pokeBodyPreviewLimit {
		raw = raw[:pokeBodyPreviewLimit]
		input.Truncated = true
	}
	if pokeBodyLooksTextual(input.ContentType, raw) {
		input.BodyText = strings.TrimSpace(string(bytes.ToValidUTF8(raw, []byte("?"))))
	}
	return input.Normalize(), nil
}

func normalizePokeContentType(raw string) string {
	ct, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err == nil {
		return strings.TrimSpace(strings.ToLower(ct))
	}
	return strings.TrimSpace(strings.ToLower(raw))
}

func pokeBodyLooksTextual(contentType string, raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	switch {
	case strings.HasPrefix(contentType, "text/"):
		return true
	case contentType == "application/json":
		return true
	case strings.HasSuffix(contentType, "+json"):
		return true
	case contentType == "application/xml":
		return true
	case strings.HasSuffix(contentType, "+xml"):
		return true
	case contentType == "application/x-www-form-urlencoded":
		return true
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return false
	}
	return utf8.Valid(raw)
}

func parsePokeInputValue(v any) (PokeInput, bool) {
	switch typed := v.(type) {
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

func stringFromAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func boolFromAny(v any) bool {
	b, _ := v.(bool)
	return b
}
