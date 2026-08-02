package guard

import (
	"reflect"
	"strings"
	"testing"
)

func TestRedactor_RedactsMisterMorphEnvAssignments(t *testing.T) {
	r := NewRedactor(RedactionConfig{})
	raw := "MISTER_MORPH_API_KEY=abc123\nMISTER_MORPH_LARK_SECRET: short\n"

	got, changed := r.RedactString(raw)
	if !changed {
		t.Fatal("RedactString() changed = false, want true")
	}
	for _, needle := range []string{"MISTER_MORPH_API_KEY", "MISTER_MORPH_LARK_SECRET", "abc123", "short"} {
		if strings.Contains(got, needle) {
			t.Fatalf("redacted output leaked %q: %q", needle, got)
		}
	}
	if !strings.Contains(got, "[redacted_env]=[redacted]") {
		t.Fatalf("redacted output missing env assignment placeholder: %q", got)
	}
	if !strings.Contains(got, "[redacted_env]: [redacted]") {
		t.Fatalf("redacted output missing env kv placeholder: %q", got)
	}
}

func TestRedactor_RedactsStandaloneMisterMorphEnvNames(t *testing.T) {
	r := NewRedactor(RedactionConfig{})
	raw := "echo $MISTER_MORPH_API_KEY && printenv MISTER_MORPH_SLACK_APP_TOKEN"

	got, changed := r.RedactString(raw)
	if !changed {
		t.Fatal("RedactString() changed = false, want true")
	}
	if strings.Contains(got, "MISTER_MORPH_") {
		t.Fatalf("redacted output still contains env name: %q", got)
	}
	if !strings.Contains(got, "[redacted_env]") {
		t.Fatalf("redacted output missing placeholder: %q", got)
	}
}

func TestRedactor_RedactStringDetailedReasons(t *testing.T) {
	r := NewRedactor(RedactionConfig{})
	raw := strings.Join([]string{
		"Authorization: Bearer abcdefghijklmnop",
		"token=abcdefghijklmnop",
		"jwt=aaaaaaaaaa.bbbbbbbbbb.cccccccccc",
		"-----BEGIN PRIVATE KEY-----",
		"abc",
		"-----END PRIVATE KEY-----",
	}, "\n")

	got, changed, reasons := r.RedactStringDetailed(raw)
	if !changed {
		t.Fatal("RedactStringDetailed() changed = false, want true")
	}
	wantReasons := []string{
		"redacted_private_key_block",
		"redacted_jwt",
		"redacted_bearer_token",
		"redacted_secret_value",
	}
	if !reflect.DeepEqual(reasons, wantReasons) {
		t.Fatalf("RedactStringDetailed() reasons = %v, want %v", reasons, wantReasons)
	}
	for _, needle := range []string{"token=abcdefghijklmnop", "aaaaaaaaaa.bbbbbbbbbb.cccccccccc", "\nabc\n"} {
		if strings.Contains(got, needle) {
			t.Fatalf("redacted output leaked %q: %q", needle, got)
		}
	}
}

func TestRedactor_RedactStringDetailedCustomPatternReason(t *testing.T) {
	r := NewRedactor(RedactionConfig{
		Enabled: true,
		Patterns: []RegexPattern{
			{Name: "slack webhook", Re: `https://hooks\.slack\.com/services/\S+`},
		},
	})
	raw := "post https://hooks.slack.com/services/T123/B456/SECRET"

	got, changed, reasons := r.RedactStringDetailed(raw)
	if !changed {
		t.Fatal("RedactStringDetailed() changed = false, want true")
	}
	wantReasons := []string{"redacted_custom_pattern_slack_webhook"}
	if !reflect.DeepEqual(reasons, wantReasons) {
		t.Fatalf("RedactStringDetailed() reasons = %v, want %v", reasons, wantReasons)
	}
	if strings.Contains(got, "hooks.slack.com/services") {
		t.Fatalf("redacted output leaked custom pattern match: %q", got)
	}
}

func TestGuardRedactStringOnlyWhenEnabled(t *testing.T) {
	const raw = "inspect secret-1234"
	cfg := Config{
		Redaction: RedactionConfig{
			Enabled: true,
			Patterns: []RegexPattern{
				{Name: "reasoning secret", Re: `secret-[0-9]+`},
			},
		},
	}

	for _, tc := range []struct {
		name        string
		enabled     bool
		want        string
		wantChanged bool
	}{
		{name: "disabled", want: raw},
		{name: "enabled", enabled: true, want: "inspect [redacted]", wantChanged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg.Enabled = tc.enabled
			g := New(cfg, nil, nil)
			got, changed := g.RedactString(raw)
			if got != tc.want || changed != tc.wantChanged {
				t.Fatalf("RedactString() = %q, %v; want %q, %v", got, changed, tc.want, tc.wantChanged)
			}
		})
	}
}
