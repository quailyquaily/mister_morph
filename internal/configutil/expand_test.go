package configutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestReadExpandedConfig(t *testing.T) {
	t.Setenv("TEST_SECRET", "hunter2")
	t.Setenv("TEST_TOKEN", "tok-abc")

	yaml := `
plain: hello
with_env: "${TEST_SECRET}"
nested:
  key: "${TEST_TOKEN}"
no_dollar: world
items:
  - name: a
    token: "${TEST_SECRET}"
port: 8080
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	v := viper.New()
	if err := ReadExpandedConfig(v, path, nil); err != nil {
		t.Fatalf("ReadExpandedConfig() error = %v", err)
	}

	tests := []struct {
		key  string
		want string
	}{
		{"plain", "hello"},
		{"with_env", "hunter2"},
		{"nested.key", "tok-abc"},
		{"no_dollar", "world"},
	}
	for _, tt := range tests {
		if got := v.GetString(tt.key); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.key, got, tt.want)
		}
	}

	if got := v.GetInt("port"); got != 8080 {
		t.Fatalf("port = %d, want 8080", got)
	}

	items := v.Get("items")
	slice, ok := items.([]any)
	if !ok || len(slice) == 0 {
		t.Fatalf("expected non-empty slice, got %T %v", items, items)
	}
	m, ok := slice[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", slice[0])
	}
	if m["token"] != "hunter2" {
		t.Fatalf("items[0].token = %q, want hunter2", m["token"])
	}
}

func TestReadExpandedConfig_PreservesLiteralDollar(t *testing.T) {
	yaml := `
regex_pattern: "password=(.+)$"
bare_var: "$HOME_SHOULD_NOT_EXPAND"
bcrypt_hash: "$2a$10$abcdefghijklmnopqrstu"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	v := viper.New()
	if err := ReadExpandedConfig(v, path, nil); err != nil {
		t.Fatalf("ReadExpandedConfig() error = %v", err)
	}

	if got := v.GetString("regex_pattern"); got != "password=(.+)$" {
		t.Errorf("regex_pattern = %q, want %q", got, "password=(.+)$")
	}
	if got := v.GetString("bare_var"); got != "$HOME_SHOULD_NOT_EXPAND" {
		t.Errorf("bare_var = %q, want %q (bare $VAR must not be expanded)", got, "$HOME_SHOULD_NOT_EXPAND")
	}
	if got := v.GetString("bcrypt_hash"); got != "$2a$10$abcdefghijklmnopqrstu" {
		t.Errorf("bcrypt_hash = %q, want %q (bcrypt hashes must not be mangled)", got, "$2a$10$abcdefghijklmnopqrstu")
	}
}

func TestReadExpandedConfig_UnsetVarWarns(t *testing.T) {
	yaml := `
key: "${UNSET_VAR_XYZ_NEVER_SET}"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	warnf := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}

	v := viper.New()
	if err := ReadExpandedConfig(v, path, warnf); err != nil {
		t.Fatalf("ReadExpandedConfig() unexpected error = %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for unset env var reference")
	}
	if !strings.Contains(warnings[0], "UNSET_VAR_XYZ_NEVER_SET") {
		t.Fatalf("warning should mention the unset var name, got: %v", warnings[0])
	}
	if got := v.GetString("key"); got != "" {
		t.Errorf("key = %q, want empty (unset var should expand to empty)", got)
	}
}

func TestReadExpandedConfig_FileNotFound(t *testing.T) {
	v := viper.New()
	err := ReadExpandedConfig(v, "/tmp/nonexistent_config_xyz.yaml", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestExpandStrictEnv(t *testing.T) {
	t.Setenv("MY_VAR", "hello")

	tests := []struct {
		name        string
		input       string
		want        string
		wantMissing []string
	}{
		{"braced var", "${MY_VAR}", "hello", nil},
		{"bare var untouched", "$MY_VAR stays", "$MY_VAR stays", nil},
		{"bcrypt hash", "$2a$10$xyz", "$2a$10$xyz", nil},
		{"missing var", "${NO_SUCH_VAR}", "", []string{"NO_SUCH_VAR"}},
		{"mixed", "${MY_VAR} and $BARE", "hello and $BARE", nil},
		{"empty braces", "${}", "${}", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, missing := expandStrictEnv(tt.input)
			if got != tt.want {
				t.Errorf("expandStrictEnv(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if len(missing) != len(tt.wantMissing) {
				t.Errorf("missing = %v, want %v", missing, tt.wantMissing)
			}
		})
	}
}
