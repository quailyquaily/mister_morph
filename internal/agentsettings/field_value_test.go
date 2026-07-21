package agentsettings

import (
	"context"
	"errors"
	"testing"
)

type fieldValueTestSource struct {
	env     map[string]string
	secrets map[string]string
	errs    map[string]error
}

func (s fieldValueTestSource) LookupEnv(name string) (string, bool) {
	value, ok := s.env[name]
	return value, ok
}

func (s fieldValueTestSource) GetAWSSecretString(_ context.Context, secretID string) (string, error) {
	if err := s.errs[secretID]; err != nil {
		return "", err
	}
	value, ok := s.secrets[secretID]
	if !ok {
		return "", errors.New("missing test secret")
	}
	return value, nil
}

func TestResolveConnectionTestFieldValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		source  fieldValueTestSource
		want    string
		wantErr string
	}{
		{name: "plain", value: "  test-value  ", want: "test-value"},
		{name: "environment", value: "${TEST_API_KEY}", source: fieldValueTestSource{env: map[string]string{"TEST_API_KEY": "env-value"}}, want: "env-value"},
		{name: "missing environment", value: "${MISSING_API_KEY}", wantErr: `missing env "MISSING_API_KEY"`},
		{name: "aws secret", value: "${aws-sm:service/api-key}", source: fieldValueTestSource{secrets: map[string]string{"service/api-key": "secret-value"}}, want: "secret-value"},
		{name: "aws failure", value: "${aws-sm:service/missing}", source: fieldValueTestSource{errs: map[string]error{"service/missing": errors.New("unavailable")}}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveConnectionTestFieldValue(tt.value, tt.source)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("ResolveConnectionTestFieldValue() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveConnectionTestFieldValue() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveConnectionTestFieldValue() = %q, want %q", got, tt.want)
			}
		})
	}
}
