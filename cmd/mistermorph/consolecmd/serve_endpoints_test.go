package consolecmd

import (
	"testing"
)

func TestResolveRuntimeEndpointsForServeSkipsInvalidEntries(t *testing.T) {
	out, warnings := resolveRuntimeEndpointsForServe([]runtimeEndpointConfigRaw{
		{Name: "Telegram", URL: "http://127.0.0.1:8787", AuthToken: "alpha"},
		{Name: "Broken", URL: "http://127.0.0.1:8788"},
		{Name: "Telegram", URL: "http://127.0.0.1:8787", AuthToken: "dupe"},
	})
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	if out[0].Name != "Telegram" {
		t.Fatalf("out[0].Name = %q, want Telegram", out[0].Name)
	}
	if len(warnings) != 2 {
		t.Fatalf("len(warnings) = %d, want 2", len(warnings))
	}
}
