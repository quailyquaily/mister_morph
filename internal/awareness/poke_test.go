package awareness

import "testing"

func TestPokeInputMetaRoundTrip(t *testing.T) {
	original := PokeInput{
		ContentType: " Application/JSON; charset=utf-8 ",
		BodyText:    "  {\"task\":\"check\"}  ",
		Truncated:   true,
		HasBody:     true,
	}
	meta := map[string]any{"awareness": map[string]any{"poke": original.MetaValue()}}

	got, ok := PokeInputFromMeta(meta)
	if !ok {
		t.Fatal("PokeInputFromMeta() ok = false")
	}
	if got.ContentType != "application/json" || got.BodyText != `{"task":"check"}` || !got.Truncated || !got.HasBody {
		t.Fatalf("PokeInputFromMeta() = %#v", got)
	}
}

func TestPokeInputFromMetaReadsLegacyHeartbeat(t *testing.T) {
	original := PokeInput{
		ContentType: "text/plain",
		BodyText:    "wake up",
		HasBody:     true,
	}

	got, ok := PokeInputFromMeta(map[string]any{
		"heartbeat": map[string]any{"poke": original.MetaValue()},
	})
	if !ok {
		t.Fatal("PokeInputFromMeta() ok = false")
	}
	if got != original.Normalize() {
		t.Fatalf("PokeInputFromMeta() = %#v, want %#v", got, original.Normalize())
	}
}

func TestPokeInputNormalizeClearsBodyFieldsWithoutBody(t *testing.T) {
	got := (PokeInput{ContentType: "text/plain", BodyText: "payload", Truncated: true}).Normalize()
	if !got.IsZero() {
		t.Fatalf("Normalize() = %#v, want zero input", got)
	}
	if got.ContentType != "" || got.BodyText != "" || got.Truncated {
		t.Fatalf("Normalize() retained body fields: %#v", got)
	}
}
