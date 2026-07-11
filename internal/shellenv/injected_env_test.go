package shellenv

import (
	"testing"
)

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "FOO_BAR", want: "FOO_BAR"},
		{in: " FOO_BAR ", want: "FOO_BAR"},
		{in: "FOO1", want: "FOO1"},
		{in: "1FOO", want: ""},
		{in: "FOO-BAR", want: ""},
		{in: "FOO BAR", want: ""},
	}
	for _, tc := range cases {
		if got := NormalizeName(tc.in); got != tc.want {
			t.Fatalf("NormalizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseInjectedEnvVars_StringList(t *testing.T) {
	t.Setenv("ENV_A", "value-a")
	t.Setenv("ENV_B", "value-b")

	got, err := ParseInjectedEnvVars([]any{"ENV_A", " ENV_B "})
	if err != nil {
		t.Fatalf("ParseInjectedEnvVars() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "ENV_A" || got[0].Value != "value-a" {
		t.Fatalf("got[0] = %+v, want ENV_A=value-a", got[0])
	}
	if got[1].Name != "ENV_B" || got[1].Value != "value-b" {
		t.Fatalf("got[1] = %+v, want ENV_B=value-b", got[1])
	}
}

func TestParseInjectedEnvVars_MixedList(t *testing.T) {
	t.Setenv("ENV_A", "from-parent")

	raw := []any{
		"ENV_A",
		map[string]any{
			"name":  "ENV_B",
			"value": "xxxxxx",
		},
	}
	got, err := ParseInjectedEnvVars(raw)
	if err != nil {
		t.Fatalf("ParseInjectedEnvVars() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "ENV_A" || got[0].Value != "from-parent" {
		t.Fatalf("got[0] = %+v, want ENV_A=from-parent", got[0])
	}
	if got[1].Name != "ENV_B" || got[1].Value != "xxxxxx" {
		t.Fatalf("got[1] = %+v, want ENV_B=xxxxxx", got[1])
	}
}

func TestParseInjectedEnvVars_ObjectWithoutValue(t *testing.T) {
	t.Setenv("ENV_A", "from-parent")

	got, err := ParseInjectedEnvVars([]any{
		map[string]any{"name": "ENV_A"},
	})
	if err != nil {
		t.Fatalf("ParseInjectedEnvVars() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "ENV_A" || got[0].Value != "from-parent" {
		t.Fatalf("got[0] = %+v, want ENV_A=from-parent", got[0])
	}
}

func TestParseInjectedEnvVars_ObjectWithEmptyValue(t *testing.T) {
	got, err := ParseInjectedEnvVars([]any{
		map[string]any{
			"name":  "ENV_A",
			"value": "",
		},
	})
	if err != nil {
		t.Fatalf("ParseInjectedEnvVars() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "ENV_A" || got[0].Value != "" {
		t.Fatalf("got[0] = %+v, want ENV_A=\"\"", got[0])
	}
}

func TestParseInjectedEnvVars_SkipsInvalidNames(t *testing.T) {
	t.Setenv("GOOD_VAR", "ok")

	got, err := ParseInjectedEnvVars([]any{"1BAD", "GOOD_VAR"})
	if err != nil {
		t.Fatalf("ParseInjectedEnvVars() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "GOOD_VAR" || got[0].Value != "ok" {
		t.Fatalf("got = %+v, want only GOOD_VAR=ok", got)
	}
}

func TestParseInjectedEnvVars_SkipsMissingParentEnv(t *testing.T) {
	got, err := ParseInjectedEnvVars([]any{"MISSING_ENV"})
	if err != nil {
		t.Fatalf("ParseInjectedEnvVars() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v, want empty list", got)
	}
}

func TestParseInjectedEnvVars_MapStringStringSlice(t *testing.T) {
	got, err := ParseInjectedEnvVars([]map[string]string{{
		"name":  "ENV_B",
		"value": "fixed-value",
	}})
	if err != nil {
		t.Fatalf("ParseInjectedEnvVars() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "ENV_B" || got[0].Value != "fixed-value" {
		t.Fatalf("got = %+v, want ENV_B=fixed-value", got)
	}
}

func TestParseInjectedEnvVars_TopLevelString(t *testing.T) {
	t.Setenv("CUSTOM_ENV", "from-parent")

	got, err := ParseInjectedEnvVars("CUSTOM_ENV")
	if err != nil {
		t.Fatalf("ParseInjectedEnvVars() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "CUSTOM_ENV" || got[0].Value != "from-parent" {
		t.Fatalf("got = %+v, want CUSTOM_ENV=from-parent", got)
	}
}

func TestInjectedEnvVarsFromConfig_AcceptsTopLevelString(t *testing.T) {
	t.Setenv("CUSTOM_ENV", "from-parent")

	got := InjectedEnvVarsFromConfig("CUSTOM_ENV")
	if len(got) != 1 || got[0].Name != "CUSTOM_ENV" || got[0].Value != "from-parent" {
		t.Fatalf("got = %+v, want CUSTOM_ENV=from-parent", got)
	}
}

func TestParseInjectedEnvVars_InvalidTopLevel(t *testing.T) {
	_, err := ParseInjectedEnvVars(123)
	if err == nil {
		t.Fatal("expected error for non-list input")
	}
}

func TestCloneInjectedEnvVars(t *testing.T) {
	in := []InjectedEnvVar{{Name: "A", Value: "1"}}
	out := CloneInjectedEnvVars(in)
	if len(out) != 1 || out[0].Name != "A" || out[0].Value != "1" {
		t.Fatalf("CloneInjectedEnvVars() = %+v", out)
	}
	out[0].Name = "B"
	if in[0].Name != "A" {
		t.Fatalf("clone mutated source: %+v", in)
	}
}

func TestMergeInjectedEnvVarsTaskOverridesLast(t *testing.T) {
	base := []InjectedEnvVar{
		{Name: "API_KEY", Value: "global"},
		{Name: "MODE", Value: "default"},
	}
	extra := []InjectedEnvVar{{Name: "API_KEY", Value: "task"}}
	got := MergeInjectedEnvVars(base, extra)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "MODE" || got[0].Value != "default" {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if got[1].Name != "API_KEY" || got[1].Value != "task" {
		t.Fatalf("got[1] = %#v", got[1])
	}
}
