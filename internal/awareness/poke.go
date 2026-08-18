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
	if !in.HasBody {
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

func normalizeContentType(raw string) string {
	contentType, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err == nil {
		return strings.TrimSpace(strings.ToLower(contentType))
	}
	return strings.TrimSpace(strings.ToLower(raw))
}
