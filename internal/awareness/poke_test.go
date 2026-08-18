package awareness

import "testing"

func TestPokeInputNormalizeClearsBodyFieldsWithoutBody(t *testing.T) {
	got := (PokeInput{ContentType: "text/plain", BodyText: "payload", Truncated: true}).Normalize()
	if !got.IsZero() {
		t.Fatalf("Normalize() = %#v, want zero input", got)
	}
	if got.ContentType != "" || got.BodyText != "" || got.Truncated {
		t.Fatalf("Normalize() retained body fields: %#v", got)
	}
}
