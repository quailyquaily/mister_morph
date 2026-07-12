package secref

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeSource struct {
	env     map[string]string
	secrets map[string]string
	errs    map[string]error
	calls   map[string]int
}

func (f *fakeSource) LookupEnv(name string) (string, bool) {
	value, ok := f.env[name]
	return value, ok
}

func (f *fakeSource) GetAWSSecretString(_ context.Context, secretID string) (string, error) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[secretID]++
	if err := f.errs[secretID]; err != nil {
		return "", err
	}
	value, ok := f.secrets[secretID]
	if !ok {
		return "", ErrAWSSecretNotFound
	}
	return value, nil
}

func TestResolveStringEnvAndAWSRefs(t *testing.T) {
	src := &fakeSource{
		env: map[string]string{
			"LOCAL_TOKEN": "local-secret",
		},
		secrets: map[string]string{
			"mistermorph/openai": "aws-secret",
		},
	}
	resolver := NewResolver(src)

	got, err := resolver.ResolveString(context.Background(), "env=${LOCAL_TOKEN}; aws=${aws-sm:mistermorph/openai}", Options{
		EnvMissing: EnvMissingWarn,
	})
	if err != nil {
		t.Fatalf("ResolveString() error = %v", err)
	}
	if got.Value != "env=local-secret; aws=aws-secret" {
		t.Fatalf("Value = %q, want expanded env and aws refs", got.Value)
	}
	if len(got.Warnings) != 0 || len(got.MissingEnv) != 0 {
		t.Fatalf("unexpected warnings/missing env: %+v", got)
	}
}

func TestResolveStringAWSJSONField(t *testing.T) {
	src := &fakeSource{
		secrets: map[string]string{
			"mistermorph/jsonbill": `{"api_key":"json-secret","count":2}`,
		},
	}
	resolver := NewResolver(src)

	got, err := resolver.ResolveString(context.Background(), "${aws-sm:mistermorph/jsonbill#api_key}", Options{})
	if err != nil {
		t.Fatalf("ResolveString() error = %v", err)
	}
	if got.Value != "json-secret" {
		t.Fatalf("Value = %q, want json field", got.Value)
	}
}

func TestResolveStringAWSFailureWarnsAndExpandsEmpty(t *testing.T) {
	src := &fakeSource{
		errs: map[string]error{
			"mistermorph/missing": errors.New("boom with sk-should-not-leak"),
		},
	}
	resolver := NewResolver(src)

	got, err := resolver.ResolveString(context.Background(), "x=${aws-sm:mistermorph/missing}", Options{})
	if err != nil {
		t.Fatalf("ResolveString() error = %v", err)
	}
	if got.Value != "x=" {
		t.Fatalf("Value = %q, want failed aws ref expanded to empty", got.Value)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("Warnings = %+v, want one warning", got.Warnings)
	}
	warning := got.Warnings[0].String()
	if !strings.Contains(warning, "mistermorph/missing") {
		t.Fatalf("warning should include secret id, got %q", warning)
	}
	if strings.Contains(warning, "sk-should-not-leak") {
		t.Fatalf("warning leaked secret-like error text: %q", warning)
	}
}

func TestResolveStringAWSJSONFieldFailureWarnsAndExpandsEmpty(t *testing.T) {
	src := &fakeSource{
		secrets: map[string]string{
			"mistermorph/jsonbill": `{"count":2}`,
		},
	}
	resolver := NewResolver(src)

	got, err := resolver.ResolveString(context.Background(), "${aws-sm:mistermorph/jsonbill#api_key}", Options{})
	if err != nil {
		t.Fatalf("ResolveString() error = %v", err)
	}
	if got.Value != "" {
		t.Fatalf("Value = %q, want empty string", got.Value)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("Warnings = %+v, want one warning", got.Warnings)
	}
	if warning := got.Warnings[0].String(); !strings.Contains(warning, "api_key") {
		t.Fatalf("warning should include field name, got %q", warning)
	}
}

func TestResolveStringPreservesLiteralDollarAndUnknownRefs(t *testing.T) {
	src := &fakeSource{}
	resolver := NewResolver(src)

	got, err := resolver.ResolveString(context.Background(), "$HOME ${} ${not-a-ref} $2a$10$hash", Options{})
	if err != nil {
		t.Fatalf("ResolveString() error = %v", err)
	}
	want := "$HOME ${} ${not-a-ref} $2a$10$hash"
	if got.Value != want {
		t.Fatalf("Value = %q, want %q", got.Value, want)
	}
}

func TestResolveStringMissingEnvPolicy(t *testing.T) {
	src := &fakeSource{}
	resolver := NewResolver(src)

	got, err := resolver.ResolveString(context.Background(), "${MISSING_ENV}", Options{EnvMissing: EnvMissingWarn})
	if err != nil {
		t.Fatalf("ResolveString() warn policy error = %v", err)
	}
	if got.Value != "" || len(got.MissingEnv) != 1 || got.MissingEnv[0] != "MISSING_ENV" {
		t.Fatalf("warn policy result = %+v, want empty value and missing env", got)
	}

	_, err = resolver.ResolveString(context.Background(), "${MISSING_ENV}", Options{EnvMissing: EnvMissingError})
	if err == nil || !strings.Contains(err.Error(), "MISSING_ENV") {
		t.Fatalf("error policy err = %v, want missing env error", err)
	}
}

func TestResolverMemoizesAWSSecretStrings(t *testing.T) {
	src := &fakeSource{
		secrets: map[string]string{
			"mistermorph/json": `{"a":"one","b":"two"}`,
		},
	}
	resolver := NewResolver(src)

	first, err := resolver.ResolveString(context.Background(), "${aws-sm:mistermorph/json#a}", Options{})
	if err != nil {
		t.Fatalf("first ResolveString() error = %v", err)
	}
	second, err := resolver.ResolveString(context.Background(), "${aws-sm:mistermorph/json#b}", Options{})
	if err != nil {
		t.Fatalf("second ResolveString() error = %v", err)
	}
	if first.Value != "one" || second.Value != "two" {
		t.Fatalf("values = %q/%q, want one/two", first.Value, second.Value)
	}
	if got := src.calls["mistermorph/json"]; got != 1 {
		t.Fatalf("GetAWSSecretString calls = %d, want 1", got)
	}
}
