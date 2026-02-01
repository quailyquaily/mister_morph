package agent

import "testing"

func TestShouldRedactKey_NormalizesDashesAndUnderscores(t *testing.T) {
	keys := []string{
		"api_key",
		"api-key",
		"X-API-Key",
		"x_api_key",
		"Authorization",
		"set-cookie",
		"Set_Cookie",
	}
	for _, k := range keys {
		if !shouldRedactKey(k, DefaultLogOptions().RedactKeys) {
			t.Fatalf("expected key %q to be redacted", k)
		}
	}
}
