package cron

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBashEnvRefUnmarshalNameValue(t *testing.T) {
	var task Task
	if err := yaml.Unmarshal([]byte(`
id: t1
at: "2026-05-12 09:00"
content: run
bash_env:
  - name: REPORT_MODE
    value: weekly
  - name: API_KEY
    value: "${OPENAI_API_KEY}"
`), &task); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(task.BashEnv) != 2 {
		t.Fatalf("len(bash_env) = %d, want 2", len(task.BashEnv))
	}
	if task.BashEnv[0].Name != "REPORT_MODE" || task.BashEnv[0].Value != "weekly" {
		t.Fatalf("bash_env[0] = %#v", task.BashEnv[0])
	}
	if task.BashEnv[1].Name != "API_KEY" || task.BashEnv[1].Value != "${OPENAI_API_KEY}" {
		t.Fatalf("bash_env[1] = %#v", task.BashEnv[1])
	}
}

func TestBashEnvRefUnmarshalShorthandMap(t *testing.T) {
	var task Task
	if err := yaml.Unmarshal([]byte(`
id: t1
at: "2026-05-12 09:00"
content: run
bash_env:
  - REPORT_MODE: weekly
  - AUTHORIZATION: "Bearer ${API_KEY}"
`), &task); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(task.BashEnv) != 2 {
		t.Fatalf("len(bash_env) = %d, want 2", len(task.BashEnv))
	}
	if task.BashEnv[0].Name != "REPORT_MODE" || task.BashEnv[0].Value != "weekly" {
		t.Fatalf("bash_env[0] = %#v", task.BashEnv[0])
	}
	if task.BashEnv[1].Name != "AUTHORIZATION" || task.BashEnv[1].Value != "Bearer ${API_KEY}" {
		t.Fatalf("bash_env[1] = %#v", task.BashEnv[1])
	}
}

func TestValidateTaskRejectsDuplicateBashEnvName(t *testing.T) {
	task := Task{
		ID:      "dup-env",
		At:      "2026-05-12 09:00",
		Content: "run",
		BashEnv: []BashEnvRef{
			{Name: "API_KEY", Value: "a"},
			{Name: "API_KEY", Value: "b"},
		},
	}
	err := ValidateTask(task)
	if err == nil || !strings.Contains(err.Error(), "duplicate bash_env name") {
		t.Fatalf("ValidateTask() = %v, want duplicate bash_env error", err)
	}
}

func TestResolveBashEnvRefsLiteralAndInterpolation(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("API_KEY", "token-123")

	got, err := ResolveBashEnvRefs([]BashEnvRef{
		{Name: "REPORT_MODE", Value: "weekly"},
		{Name: "API_KEY", Value: "${OPENAI_API_KEY}"},
		{Name: "AUTHORIZATION", Value: "Bearer ${API_KEY}"},
	})
	if err != nil {
		t.Fatalf("ResolveBashEnvRefs() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Name != "REPORT_MODE" || got[0].Value != "weekly" {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if got[1].Value != "sk-test" {
		t.Fatalf("got[1].Value = %q", got[1].Value)
	}
	if got[2].Value != "Bearer token-123" {
		t.Fatalf("got[2].Value = %q", got[2].Value)
	}
}

func TestResolveBashEnvRefsMissingEnv(t *testing.T) {
	_ = os.Unsetenv("MISSING_BASH_ENV_XYZ")
	_, err := ResolveBashEnvRefs([]BashEnvRef{{Name: "API_KEY", Value: "${MISSING_BASH_ENV_XYZ}"}})
	if err == nil || !strings.Contains(err.Error(), "MISSING_BASH_ENV_XYZ") {
		t.Fatalf("ResolveBashEnvRefs() = %v, want missing env error", err)
	}
	if strings.Contains(err.Error(), "sk-") {
		t.Fatalf("error should not echo secret values: %v", err)
	}
}
