package daemonruntime

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadPokeInput_TextBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/poke", strings.NewReader("{\"reason\":\"deploy\"}"))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	input, err := readPokeInput(req)
	if err != nil {
		t.Fatalf("readPokeInput() error = %v", err)
	}
	if !input.HasBody {
		t.Fatalf("expected has_body=true: %#v", input)
	}
	if input.ContentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", input.ContentType)
	}
	if input.BodyText != "{\"reason\":\"deploy\"}" {
		t.Fatalf("body text = %q, want JSON body", input.BodyText)
	}
	if input.Truncated {
		t.Fatalf("truncated = true, want false")
	}
}

func TestPokeInputMetaRoundTrip(t *testing.T) {
	original := PokeInput{
		ContentType: "text/plain",
		BodyText:    "wake up",
		HasBody:     true,
		Truncated:   true,
	}

	meta := map[string]any{
		"awareness": map[string]any{
			"poke": original.MetaValue(),
		},
	}
	got, ok := PokeInputFromMeta(meta)
	if !ok {
		t.Fatalf("expected poke input in meta")
	}
	if got != original.Normalize() {
		t.Fatalf("poke input = %#v, want %#v", got, original.Normalize())
	}
}

func TestPokeInputMetaRoundTripLegacyHeartbeat(t *testing.T) {
	original := PokeInput{
		ContentType: "text/plain",
		BodyText:    "wake up",
		HasBody:     true,
	}

	meta := map[string]any{
		"heartbeat": map[string]any{
			"poke": original.MetaValue(),
		},
	}
	got, ok := PokeInputFromMeta(meta)
	if !ok {
		t.Fatalf("expected poke input in legacy heartbeat meta")
	}
	if got != original.Normalize() {
		t.Fatalf("poke input = %#v, want %#v", got, original.Normalize())
	}
}
