package configutil

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectConfigEnvRefsReportsSortedUniqueStatuses(t *testing.T) {
	t.Setenv("DOCTOR_ENV_SET", "secret")
	t.Setenv("DOCTOR_ENV_EMPTY", "   ")
	t.Setenv("DOCTOR_ENV_MISSING", "temporary")
	if err := os.Unsetenv("DOCTOR_ENV_MISSING"); err != nil {
		t.Fatalf("Unsetenv() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `
llm:
  api_key: "${DOCTOR_ENV_SET}"
  endpoint: "https://${DOCTOR_ENV_MISSING}/v1"
telegram:
  token: "${DOCTOR_ENV_EMPTY}"
duplicate: "${DOCTOR_ENV_SET}"
aws: "${aws-sm:service/key}"
"${DOCTOR_ENV_KEY}": ignored
# comment: "${DOCTOR_ENV_COMMENT}"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := InspectConfigEnvRefs(path)
	if err != nil {
		t.Fatalf("InspectConfigEnvRefs() error = %v", err)
	}
	want := []EnvRefCheck{
		{Name: "DOCTOR_ENV_EMPTY", Status: EnvRefEmpty},
		{Name: "DOCTOR_ENV_MISSING", Status: EnvRefMissing},
		{Name: "DOCTOR_ENV_SET", Status: EnvRefSet},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("InspectConfigEnvRefs() = %#v, want %#v", got, want)
	}
}

func TestInspectConfigEnvRefsRejectsInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("llm: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := InspectConfigEnvRefs(path); err == nil {
		t.Fatal("InspectConfigEnvRefs() error = nil, want invalid YAML error")
	}
}
