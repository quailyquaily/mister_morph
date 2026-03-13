package configutil

import (
	"testing"

	"github.com/spf13/viper"
)

func TestExpandEnvStrings(t *testing.T) {
	t.Setenv("TEST_SECRET", "hunter2")
	t.Setenv("TEST_TOKEN", "tok-abc")

	v := viper.New()
	v.Set("plain", "hello")
	v.Set("with_env", "${TEST_SECRET}")
	v.Set("nested.key", "${TEST_TOKEN}")
	v.Set("no_dollar", "world")

	ExpandEnvStrings(v)

	if got := v.GetString("plain"); got != "hello" {
		t.Fatalf("plain = %q, want hello", got)
	}
	if got := v.GetString("with_env"); got != "hunter2" {
		t.Fatalf("with_env = %q, want hunter2", got)
	}
	if got := v.GetString("nested.key"); got != "tok-abc" {
		t.Fatalf("nested.key = %q, want tok-abc", got)
	}
	if got := v.GetString("no_dollar"); got != "world" {
		t.Fatalf("no_dollar = %q, want world", got)
	}
}

func TestExpandEnvStrings_Slice(t *testing.T) {
	t.Setenv("TEST_ITEM", "resolved")

	v := viper.New()
	v.Set("items", []any{
		map[string]any{
			"name":  "a",
			"token": "${TEST_ITEM}",
		},
	})

	ExpandEnvStrings(v)

	items := v.Get("items")
	slice, ok := items.([]any)
	if !ok || len(slice) == 0 {
		t.Fatalf("expected non-empty slice, got %T %v", items, items)
	}
	m, ok := slice[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", slice[0])
	}
	if m["token"] != "resolved" {
		t.Fatalf("token = %q, want resolved", m["token"])
	}
}

func TestExpandEnvStrings_Nil(t *testing.T) {
	ExpandEnvStrings(nil)
}
