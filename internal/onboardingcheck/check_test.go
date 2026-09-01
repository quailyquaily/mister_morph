package onboardingcheck

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/secref"
)

type checkSecretSource struct {
	err error
}

func (checkSecretSource) LookupEnv(string) (string, bool) { return "", false }

func (checkSecretSource) GetAWSSecretString(context.Context, string) (string, error) {
	return "", secref.ErrAWSSecretNotFound
}

func (s checkSecretSource) GetOSSecretString(context.Context, string) (string, error) {
	return "", s.err
}

func TestInspectConfigPath(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("llm:\n  provider: openai\n  model: gpt-5.2\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		got := InspectConfigPath(path, nil)
		if got.Status != StatusOK {
			t.Fatalf("Status = %q, want %q (%s)", got.Status, StatusOK, got.Error)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("llm: [\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		got := InspectConfigPath(path, nil)
		if got.Status != StatusMalformed {
			t.Fatalf("Status = %q, want %q", got.Status, StatusMalformed)
		}
		if got.Code != CodeConfigInvalid {
			t.Fatalf("Code = %q, want %q", got.Code, CodeConfigInvalid)
		}
	})
}

func TestInspectConfigPathClassifiesSecretReferenceFailures(t *testing.T) {
	const id = "b_LsX7HLzAR3OShG7YjRcw"
	tests := []struct {
		name     string
		value    string
		source   checkSecretSource
		want     string
		wantPath []string
	}{
		{
			name:     "missing secret",
			value:    "${secret:" + id + "}",
			source:   checkSecretSource{err: secref.ErrOSSecretNotFound},
			want:     CodeOSSecretNotFound,
			wantPath: []string{"llm", "profiles", "main", "api_key"},
		},
		{
			name:     "unavailable store",
			value:    "${secret:" + id + "}",
			source:   checkSecretSource{err: secref.ErrOSStoreUnavailable},
			want:     CodeOSSecretStoreUnavailable,
			wantPath: []string{"llm", "profiles", "main", "api_key"},
		},
		{
			name:     "invalid reference",
			value:    "${secret:short}",
			source:   checkSecretSource{},
			want:     CodeInvalidSecretRef,
			wantPath: []string{"llm", "profiles", "main", "api_key"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			raw := "llm:\n  profiles:\n    main:\n      api_key: \"" + tt.value + "\"\n"
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			got := InspectConfigPath(path, tt.source)
			if got.Code != tt.want {
				t.Fatalf("Code = %q, want %q (%s)", got.Code, tt.want, got.Error)
			}
			if !reflect.DeepEqual(got.FieldPath, tt.wantPath) {
				t.Fatalf("FieldPath = %v, want %v", got.FieldPath, tt.wantPath)
			}
		})
	}
}

func TestValidateIdentityYAML(t *testing.T) {
	if err := ValidateIdentityYAML("name: Momo\ncreature: cat\nvibe: calm\nemoji: cat\n"); err != nil {
		t.Fatalf("ValidateIdentityYAML() error = %v", err)
	}
	if err := ValidateIdentityYAML("- name: Momo\n"); err == nil {
		t.Fatalf("ValidateIdentityYAML() error = nil, want malformed")
	}
}

func TestValidateSoulMarkdown(t *testing.T) {
	if err := ValidateSoulMarkdown("# soul.md\n\n## Core Truths\n- A\n\n## Boundaries\n- B\n\n## Vibe\n\nC\n"); err != nil {
		t.Fatalf("ValidateSoulMarkdown() error = %v", err)
	}
	if err := ValidateSoulMarkdown("# soul.md\n\n## Vibe\n\nC\n"); err == nil {
		t.Fatalf("ValidateSoulMarkdown() error = nil, want malformed")
	}
}
