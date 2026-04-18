package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"
)

type PowerShellTool struct {
	Enabled         bool
	DefaultTimeout  time.Duration
	MaxOutputBytes  int
	BaseDirs        []string
	DenyPaths       []string
	DenyTokens      []string
	InjectedEnvVars []string
}

func NewPowerShellTool(enabled bool, defaultTimeout time.Duration, maxOutputBytes int, baseDirs ...string) *PowerShellTool {
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = 256 * 1024
	}
	return &PowerShellTool{
		Enabled:        enabled,
		DefaultTimeout: defaultTimeout,
		MaxOutputBytes: maxOutputBytes,
		BaseDirs:       normalizeBaseDirs(baseDirs),
	}
}

func (t *PowerShellTool) Name() string { return "powershell" }

func (t *PowerShellTool) Description() string {
	return "Runs a PowerShell command and returns stdout/stderr. " +
		"For the `cmd` and `cwd`, supports path aliases file_cache_dir and file_state_dir."
}

func (t *PowerShellTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cmd": map[string]any{
				"type":        "string",
				"description": "PowerShell command to execute.",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Optional working directory.",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": "Optional timeout in seconds.",
			},
		},
		"required": []string{"cmd"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *PowerShellTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if !t.Enabled {
		return "", fmt.Errorf("powershell tool is disabled (enable via config: tools.powershell.enabled=true)")
	}
	return executeShellCommand(ctx, params, t.commonConfig(), t.runnerSpec())
}

func (t *PowerShellTool) commonConfig() shellToolCommon {
	return shellToolCommon{
		ToolName:        t.Name(),
		DefaultTimeout:  t.DefaultTimeout,
		MaxOutputBytes:  t.MaxOutputBytes,
		BaseDirs:        append([]string(nil), t.BaseDirs...),
		DenyPaths:       append([]string(nil), t.DenyPaths...),
		DenyTokens:      append([]string(nil), t.DenyTokens...),
		InjectedEnvVars: append([]string(nil), t.InjectedEnvVars...),
	}
}

func (t *PowerShellTool) runnerSpec() shellRunnerSpec {
	return shellRunnerSpec{
		Program:                      "powershell",
		ArgsPrefix:                   []string{"-NoProfile", "-Command"},
		BuildEnv:                     powershellToolEnv,
		TokenBoundary:                isPowerShellBoundaryByte,
		MatchDeniedPath:              powershellCommandDenied,
		TimeoutExitCode:              0,
		ReturnObservationOnExitError: true,
		ReturnObservationOnTimeout:   false,
		ReturnObservationOnExecError: false,
	}
}

func powershellToolEnv(injected []string) []string {
	env := bashToolEnv(injected)
	seen := make(map[string]bool, len(env))
	for _, e := range env {
		if i := strings.Index(e, "="); i > 0 {
			seen[strings.ToUpper(e[:i])] = true
		}
	}
	for _, key := range []string{
		"APPDATA",
		"COMSPEC",
		"LOCALAPPDATA",
		"PATHEXT",
		"PROGRAMDATA",
		"PROGRAMFILES",
		"PROGRAMFILES(X86)",
		"SYSTEMROOT",
		"TEMP",
		"TMP",
		"USERPROFILE",
		"WINDIR",
	} {
		if seen[strings.ToUpper(key)] {
			continue
		}
		value, ok := os.LookupEnv(key)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		seen[strings.ToUpper(key)] = true
		env = append(env, key+"="+value)
	}
	return env
}

func powershellCommandDenied(cmdStr string, denyPaths []string) (string, bool) {
	cmdStr = normalizePowerShellToken(cmdStr)
	if cmdStr == "" || len(denyPaths) == 0 {
		return "", false
	}
	for _, raw := range denyPaths {
		normalized := normalizePowerShellToken(raw)
		if normalized == "" {
			continue
		}
		if containsTokenBoundaryWithBoundary(cmdStr, normalized, isPowerShellBoundaryByte) {
			return raw, true
		}
		if base := path.Base(normalized); base != "." && base != "/" && containsTokenBoundaryWithBoundary(cmdStr, base, isPowerShellBoundaryByte) {
			return base, true
		}
	}
	return "", false
}

func normalizePowerShellToken(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.ToLower(value)
	return path.Clean(value)
}
