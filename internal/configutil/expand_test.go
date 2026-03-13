package configutil

import (
	"os"
	"path/filepath"
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
	if err := ReadExpandedConfig(v, path); err != nil {
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
literal_brace: "${}"
unset_var: "${UNSET_VAR_XYZ_NEVER_SET}"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	v := viper.New()
	if err := ReadExpandedConfig(v, path); err != nil {
		t.Fatalf("ReadExpandedConfig() error = %v", err)
	}

	if got := v.GetString("regex_pattern"); got != "password=(.+)$" {
		t.Errorf("regex_pattern = %q, want %q", got, "password=(.+)$")
	}

	if got := v.GetString("unset_var"); got != "" {
		t.Errorf("unset_var = %q, want empty", got)
	}
}

func TestReadExpandedConfig_FileNotFound(t *testing.T) {
	v := viper.New()
	err := ReadExpandedConfig(v, "/tmp/nonexistent_config_xyz.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
