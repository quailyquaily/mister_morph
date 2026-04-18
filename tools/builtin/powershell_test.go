package builtin

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPowerShellToolEnv_UsesAllowlistedEnvOnly(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HOME", "/tmp/mm-home")
	t.Setenv("LANG", "C.UTF-8")
	t.Setenv("SYSTEMROOT", `C:\Windows`)
	t.Setenv("CUSTOM_PS_ALLOWED", "https://example.com")
	t.Setenv("MISTER_MORPH_API_KEY", "secret_value_should_not_leak")

	env := strings.Join(powershellToolEnv([]string{"CUSTOM_PS_ALLOWED"}), "\n")
	if !strings.Contains(env, "HOME=/tmp/mm-home") {
		t.Fatalf("expected HOME to be preserved, got %q", env)
	}
	if !strings.Contains(env, "LANG=C.UTF-8") {
		t.Fatalf("expected LANG to be preserved, got %q", env)
	}
	if !strings.Contains(env, `SYSTEMROOT=C:\Windows`) {
		t.Fatalf("expected SYSTEMROOT to be preserved, got %q", env)
	}
	if !strings.Contains(env, "CUSTOM_PS_ALLOWED=https://example.com") {
		t.Fatalf("expected injected env var to be present, got %q", env)
	}
	if strings.Contains(env, "MISTER_MORPH_API_KEY") || strings.Contains(env, "secret_value_should_not_leak") {
		t.Fatalf("powershell env leaked mistermorph secret env: %q", env)
	}
}

func TestPowerShellCommandDenied_NormalizesWindowsPaths(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		deny string
		want bool
	}{
		{name: "basename with backslashes", cmd: `Get-Content C:\tmp\config.yaml`, deny: "config.yaml", want: true},
		{name: "full path with backslashes", cmd: `Get-Content C:\tmp\config.yaml`, deny: `C:\tmp\config.yaml`, want: true},
		{name: "case insensitive", cmd: `Get-Content C:\TMP\CONFIG.YAML`, deny: "config.yaml", want: true},
		{name: "nonmatch suffix", cmd: `Get-Content C:\tmp\config.yaml.bak`, deny: "config.yaml", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := powershellCommandDenied(tc.cmd, []string{tc.deny})
			if ok != tc.want {
				t.Fatalf("powershellCommandDenied(%q, %q) = %v, want %v", tc.cmd, tc.deny, ok, tc.want)
			}
		})
	}
}

func TestPrepareShellInvocation_PowerShellAliasSupportsBackslashes(t *testing.T) {
	cache := t.TempDir()

	inv, err := prepareShellInvocation(map[string]any{
		"cmd": `Get-Content file_cache_dir\notes.txt`,
	}, shellToolCommon{
		ToolName:       "powershell",
		DefaultTimeout: 5 * time.Second,
		BaseDirs:       []string{cache},
	}, shellRunnerSpec{
		TokenBoundary: isPowerShellBoundaryByte,
	})
	if err != nil {
		t.Fatalf("prepareShellInvocation() error = %v", err)
	}

	want := `Get-Content ` + filepath.Clean(cache) + `\notes.txt`
	if inv.Command != want {
		t.Fatalf("inv.Command = %q, want %q", inv.Command, want)
	}
}
