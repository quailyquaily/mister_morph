package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestRunDoctorReportsEnvRefStatusesWithoutValues(t *testing.T) {
	t.Setenv("DOCTOR_COMMAND_SET", "do-not-print-this")
	t.Setenv("DOCTOR_COMMAND_EMPTY", "")
	t.Setenv("DOCTOR_COMMAND_MISSING", "temporary")
	if err := os.Unsetenv("DOCTOR_COMMAND_MISSING"); err != nil {
		t.Fatalf("Unsetenv() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `
set: "${DOCTOR_COMMAND_SET}"
empty: "${DOCTOR_COMMAND_EMPTY}"
missing: "${DOCTOR_COMMAND_MISSING}"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	err := runDoctor(&out, path, http.DefaultClient)
	if err == nil {
		t.Fatal("runDoctor() error = nil, want unhealthy config error")
	}
	got := out.String()
	for _, want := range []string{
		"MisterMorph doctor",
		"Environment references",
		"DOCTOR_COMMAND_EMPTY",
		"EMPTY",
		"DOCTOR_COMMAND_MISSING",
		"MISSING",
		"DOCTOR_COMMAND_SET",
		"OK",
		"Summary",
		"FAILED",
		"Problems",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
	if !outputLineHasFields(got, "Problems", "2") {
		t.Fatalf("doctor output does not report two problems:\n%s", got)
	}
	if strings.Contains(got, "do-not-print-this") {
		t.Fatalf("doctor output leaked an environment value:\n%s", got)
	}
}

func TestRunDoctorReportsHealthyConfigWithoutEnvRefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("llm:\n  model: gpt-5.6\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	if err := runDoctor(&out, path, http.DefaultClient); err != nil {
		t.Fatalf("runDoctor() error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Environment references") ||
		!outputLineHasFields(got, "Status", "OK") ||
		!outputLineHasFields(got, "Problems", "0") {
		t.Fatalf("unexpected doctor output:\n%s", got)
	}
}

func TestRunDoctorReportsMissingAndInvalidConfigErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "missing path", want: "config file not found; pass --config"},
		{name: "invalid yaml", path: filepath.Join(t.TempDir(), "invalid.yaml"), want: "inspect config:"},
	}
	if err := os.WriteFile(tests[1].path, []byte("llm: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := runDoctor(&out, tt.path, http.DefaultClient); err == nil {
				t.Fatal("runDoctor() error = nil, want error")
			}
			if got := out.String(); !strings.Contains(got, tt.want) {
				t.Fatalf("doctor output missing %q:\n%s", tt.want, got)
			}
		})
	}
}

func TestRunDoctorChecksManagedSlackCredentialsAndScopes(t *testing.T) {
	const (
		botToken = "xoxb-doctor-secret"
		appToken = "xapp-doctor-secret"
	)
	t.Setenv("DOCTOR_SLACK_BOT_TOKEN", botToken)
	t.Setenv("DOCTOR_SLACK_APP_TOKEN", appToken)

	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			if got := r.Header.Get("Authorization"); got != "Bearer "+botToken {
				t.Errorf("auth.test Authorization = %q", got)
			}
			w.Header().Set("X-OAuth-Scopes", strings.Join([]string{
				"app_mentions:read",
				"channels:history",
				"channels:read",
				"chat:write",
				"emoji:read",
				"files:read",
				"files:write",
				"groups:history",
				"groups:read",
				"im:history",
				"im:read",
				"mpim:history",
				"mpim:read",
				"reactions:write",
				"users:read",
			}, ","))
			_, _ = fmt.Fprint(w, `{"ok":true,"team_id":"T123"}`)
		case "/apps.connections.open":
			if got := r.Header.Get("Authorization"); got != "Bearer "+appToken {
				t.Errorf("apps.connections.open Authorization = %q", got)
			}
			_, _ = fmt.Fprint(w, `{"ok":true,"url":"wss://example.invalid/socket"}`)
		default:
			http.NotFound(w, r)
		}
	}))

	path := filepath.Join(t.TempDir(), "config.yaml")
	body := fmt.Sprintf(`
console:
  managed_runtimes: [slack]
slack:
  base_url: %q
  bot_token: "${DOCTOR_SLACK_BOT_TOKEN}"
  app_token: "${DOCTOR_SLACK_APP_TOKEN}"
`, server.URL)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	if err := runDoctor(&out, path, server.Client); err != nil {
		t.Fatalf("runDoctor() error = %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Slack",
		"enabled (managed)",
		"Bot token",
		"Bot scopes",
		"App token",
		"Summary",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
	for _, fields := range [][]string{
		{"Bot", "token", "OK"},
		{"Bot", "scopes", "OK"},
		{"App", "token", "OK"},
		{"Status", "OK"},
		{"Problems", "0"},
	} {
		if !outputLineHasFields(got, fields...) {
			t.Fatalf("doctor output is missing line with fields %q:\n%s", fields, got)
		}
	}
	if strings.Contains(got, botToken) || strings.Contains(got, appToken) {
		t.Fatalf("doctor output leaked a Slack token:\n%s", got)
	}
}

func TestRunDoctorReportsMissingSlackConfigAndBotScopes(t *testing.T) {
	const botToken = "xoxb-doctor-secret"

	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth.test" {
			t.Errorf("unexpected Slack API path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-OAuth-Scopes", strings.Join([]string{
			"app_mentions:read",
			"channels:history",
			"channels:read",
			"chat:write",
			"groups:history",
			"groups:read",
			"im:history",
			"im:read",
			"mpim:history",
			"mpim:read",
			"users:read",
		}, ","))
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))

	path := filepath.Join(t.TempDir(), "config.yaml")
	body := fmt.Sprintf(`
console:
  managed_runtimes: [slack]
slack:
  base_url: %q
  bot_token: %q
`, server.URL, botToken)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	err := runDoctor(&out, path, server.Client)
	if err == nil {
		t.Fatal("runDoctor() error = nil, want unhealthy Slack config error")
	}
	got := out.String()
	for _, want := range []string{
		"Slack",
		"enabled (managed)",
		"Bot scopes",
		"- emoji:read",
		"- files:read",
		"- files:write",
		"- reactions:write",
		"App token",
		"Summary",
		"FAILED",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
	for _, fields := range [][]string{
		{"Bot", "scopes", "MISSING"},
		{"App", "token", "MISSING"},
		{"Problems", "2"},
	} {
		if !outputLineHasFields(got, fields...) {
			t.Fatalf("doctor output is missing line with fields %q:\n%s", fields, got)
		}
	}
	if strings.Contains(got, botToken) {
		t.Fatalf("doctor output leaked the Slack bot token:\n%s", got)
	}
}

func TestRunDoctorChecksSlackWhenTokensAreConfigured(t *testing.T) {
	server := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			w.Header().Set("X-OAuth-Scopes", strings.Join(requiredSlackBotScopes, ","))
			_, _ = fmt.Fprint(w, `{"ok":true}`)
		case "/apps.connections.open":
			_, _ = fmt.Fprint(w, `{"ok":true,"url":"wss://example.invalid/socket"}`)
		default:
			http.NotFound(w, r)
		}
	}))

	path := filepath.Join(t.TempDir(), "config.yaml")
	body := fmt.Sprintf(`
slack:
  base_url: %q
  bot_token: xoxb-configured
  app_token: xapp-configured
`, server.URL)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	if err := runDoctor(&out, path, server.Client); err != nil {
		t.Fatalf("runDoctor() error = %v\n%s", err, out.String())
	}
	if got := out.String(); !strings.Contains(got, "enabled (configured)") {
		t.Fatalf("doctor did not check configured Slack runtime:\n%s", got)
	}
}

func outputLineHasFields(output string, fields ...string) bool {
	for _, line := range strings.Split(output, "\n") {
		lineFields := strings.Fields(line)
		if len(lineFields) != len(fields) {
			continue
		}
		matched := true
		for i := range fields {
			if lineFields[i] != fields[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func TestRootCommandIncludesDoctor(t *testing.T) {
	cmd := newRootCmd()
	found := false
	for _, child := range cmd.Commands() {
		if child.Name() == "doctor" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("root command is missing doctor")
	}
}
