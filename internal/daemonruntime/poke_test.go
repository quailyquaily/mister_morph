package daemonruntime

import (
	"errors"
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

func TestReadPokeInput_RejectsOversizedBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/poke", strings.NewReader(strings.Repeat("x", pokeBodyLimit+1)))
	req.Header.Set("Content-Type", "text/plain")

	input, err := readPokeInput(req)
	if !errors.Is(err, ErrPokeBodyTooLarge) {
		t.Fatalf("readPokeInput() error = %v, want ErrPokeBodyTooLarge", err)
	}
	if !input.IsZero() {
		t.Fatalf("input = %#v, want zero", input)
	}
}
