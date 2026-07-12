package configutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/secref"
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

type fakeSecretRefSource struct {
	secrets map[string]string
	errs    map[string]error
	calls   map[string]int
}

func (f fakeSecretRefSource) LookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

func (f fakeSecretRefSource) GetAWSSecretString(_ context.Context, secretID string) (string, error) {
	if f.calls != nil {
		f.calls[secretID]++
	}
	if err := f.errs[secretID]; err != nil {
		return "", err
	}
	value, ok := f.secrets[secretID]
	if !ok {
		return "", secref.ErrAWSSecretNotFound
	}
	return value, nil
}

func TestReadExpandedConfigWithSource_AWSSecretRef(t *testing.T) {
	yaml := `
llm:
  api_key: "${aws-sm:mistermorph/openai-api-key}"
auth_profiles:
  jsonbill:
    credential:
      secret: "${aws-sm:mistermorph/jsonbill#api_key}"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	src := fakeSecretRefSource{secrets: map[string]string{
		"mistermorph/openai-api-key": "sk-from-aws",
		"mistermorph/jsonbill":       `{"api_key":"jsonbill-from-aws"}`,
	}}
	v := viper.New()
	if err := ReadExpandedConfigWithSource(v, path, src, nil); err != nil {
		t.Fatalf("ReadExpandedConfigWithSource() error = %v", err)
	}
	if got := v.GetString("llm.api_key"); got != "sk-from-aws" {
		t.Fatalf("llm.api_key = %q, want AWS secret", got)
	}
	if got := v.GetString("auth_profiles.jsonbill.credential.secret"); got != "jsonbill-from-aws" {
		t.Fatalf("auth profile secret = %q, want JSON field secret", got)
	}
}

func TestReadExpandedConfigWithSource_AWSFailureWarnsAndExpandsEmpty(t *testing.T) {
	yaml := `
llm:
  api_key: "${aws-sm:mistermorph/missing}"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	warnf := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}
	src := fakeSecretRefSource{errs: map[string]error{
		"mistermorph/missing": fmt.Errorf("failed with sk-should-not-leak"),
	}}
	v := viper.New()
	if err := ReadExpandedConfigWithSource(v, path, src, warnf); err != nil {
		t.Fatalf("ReadExpandedConfigWithSource() error = %v", err)
	}
	if got := v.GetString("llm.api_key"); got != "" {
		t.Fatalf("llm.api_key = %q, want empty string", got)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for failed AWS secret ref")
	}
	if !strings.Contains(warnings[0], "mistermorph/missing") {
		t.Fatalf("warning should mention secret id, got %q", warnings[0])
	}
	if strings.Contains(warnings[0], "sk-should-not-leak") {
		t.Fatalf("warning leaked secret-like text: %q", warnings[0])
	}
}

func TestReadExpandedConfigWithSource_IgnoresAWSRefsInComments(t *testing.T) {
	yaml := `
# This is documentation only: ${aws-sm:mistermorph/comment-only}
llm:
  model: gpt-test
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	warnf := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}
	calls := map[string]int{}
	src := fakeSecretRefSource{calls: calls}

	v := viper.New()
	if err := ReadExpandedConfigWithSource(v, path, src, warnf); err != nil {
		t.Fatalf("ReadExpandedConfigWithSource() error = %v", err)
	}
	if got := v.GetString("llm.model"); got != "gpt-test" {
		t.Fatalf("llm.model = %q, want gpt-test", got)
	}
	if got := calls["mistermorph/comment-only"]; got != 0 {
		t.Fatalf("comment-only AWS ref calls = %d, want 0", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestAWSSecretsManagerConfigFromRawYAML(t *testing.T) {
	t.Setenv("AWS_SM_PROFILE_FOR_TEST", "prod")
	raw := []byte(`
secrets:
  aws_secrets_manager:
    region: us-east-1
    profile: "${AWS_SM_PROFILE_FOR_TEST}"
`)

	got := awsSecretsManagerConfigFromRawYAML(raw, nil)
	if got.Region != "us-east-1" || got.Profile != "prod" {
		t.Fatalf("awsSecretsManagerConfigFromRawYAML() = %+v, want region/profile", got)
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
