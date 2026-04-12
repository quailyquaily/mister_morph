package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
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

	cmdStr, _ := params["cmd"].(string)
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return "", fmt.Errorf("missing required param: cmd")
	}
	var err error
	cmdStr, err = t.expandPathAliasesInCommand(cmdStr)
	if err != nil {
		return "", err
	}

	if offending, ok := bashCommandDenied(cmdStr, t.DenyPaths); ok {
		return "", fmt.Errorf("powershell command references denied path %q (configure via tools.powershell.deny_paths)", offending)
	}
	if offending, ok := bashCommandDeniedTokens(cmdStr, t.DenyTokens); ok {
		return "", fmt.Errorf("powershell command references denied token %q", offending)
	}

	cwd, _ := params["cwd"].(string)
	cwd = strings.TrimSpace(cwd)
	cwd, err = t.resolveCWD(cwd)
	if err != nil {
		return "", err
	}

	timeout := t.DefaultTimeout
	if v, ok := params["timeout_seconds"]; ok {
		if secs, ok := asFloat64(v); ok && secs > 0 {
			timeout = time.Duration(secs * float64(time.Second))
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "powershell", "-NoProfile", "-Command", cmdStr)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = powershellToolEnv(t.InjectedEnvVars)

	var stdout limitedBuffer
	var stderr limitedBuffer
	stdout.Limit = t.MaxOutputBytes
	stderr.Limit = t.MaxOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else if runCtx.Err() != nil {
			return "", fmt.Errorf("powershell timed out after %s", timeout)
		} else {
			return "", err
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "exit_code: %d\n", exitCode)
	fmt.Fprintf(&b, "stdout_truncated: %t\n", stdout.Truncated)
	fmt.Fprintf(&b, "stderr_truncated: %t\n", stderr.Truncated)
	b.WriteString("stdout:\n")
	b.WriteString(string(bytes.ToValidUTF8(stdout.Bytes(), []byte("\n[non-utf8 output]\n"))))
	b.WriteString("\n\nstderr:\n")
	b.WriteString(string(bytes.ToValidUTF8(stderr.Bytes(), []byte("\n[non-utf8 output]\n"))))

	if exitCode != 0 {
		return b.String(), fmt.Errorf("powershell exited with code %d", exitCode)
	}
	return b.String(), nil
}

func powershellToolEnv(injected []string) []string {
	env := os.Environ()
	seen := make(map[string]bool)
	for _, e := range env {
		if i := strings.Index(e, "="); i > 0 {
			seen[e[:i]] = true
		}
	}
	for _, raw := range injected {
		key := normalizeInjectedEnvVarName(raw)
		if key == "" || seen[key] {
			continue
		}
		value, ok := os.LookupEnv(key)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		seen[key] = true
		env = append(env, key+"="+value)
	}
	return env
}

func (t *PowerShellTool) resolveCWD(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	alias, rest := detectWritePathAlias(raw)
	if alias == "" {
		return pathutil.ExpandHomePath(raw), nil
	}
	base := selectBaseForAlias(t.BaseDirs, alias)
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("base dir %s is not configured", alias)
	}
	rest = strings.TrimLeft(strings.TrimSpace(rest), "/\\")
	if rest == "" {
		return filepath.Clean(base), nil
	}
	return filepath.Clean(filepath.Join(base, rest)), nil
}

func (t *PowerShellTool) expandPathAliasesInCommand(cmd string) (string, error) {
	var err error
	cmd, err = replaceAliasTokenInCommand(cmd, "file_cache_dir", selectBaseForAlias(t.BaseDirs, "file_cache_dir"))
	if err != nil {
		return "", err
	}
	cmd, err = replaceAliasTokenInCommand(cmd, "file_state_dir", selectBaseForAlias(t.BaseDirs, "file_state_dir"))
	if err != nil {
		return "", err
	}
	return cmd, nil
}
