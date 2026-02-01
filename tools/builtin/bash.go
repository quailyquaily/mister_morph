package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type BashTool struct {
	Enabled        bool
	ConfirmEachRun bool
	DefaultTimeout time.Duration
	MaxOutputBytes int
	DenyPaths      []string
	DenyTokens     []string
}

func NewBashTool(enabled bool, confirmEachRun bool, defaultTimeout time.Duration, maxOutputBytes int) *BashTool {
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = 256 * 1024
	}
	return &BashTool{
		Enabled:        enabled,
		ConfirmEachRun: confirmEachRun,
		DefaultTimeout: defaultTimeout,
		MaxOutputBytes: maxOutputBytes,
	}
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	return "Runs a bash command in the local environment and returns stdout/stderr. Disabled by default for safety."
}

func (t *BashTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cmd": map[string]any{
				"type":        "string",
				"description": "Bash command to execute.",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Optional working directory.",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": "Optional timeout override in seconds.",
			},
		},
		"required": []string{"cmd"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *BashTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if !t.Enabled {
		return "", fmt.Errorf("bash tool is disabled (enable via config: tools.bash.enabled=true)")
	}

	cmdStr, _ := params["cmd"].(string)
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return "", fmt.Errorf("missing required param: cmd")
	}

	if offending, ok := bashCommandDenied(cmdStr, t.DenyPaths); ok {
		return "", fmt.Errorf("bash command references denied path %q (configure via tools.bash.deny_paths)", offending)
	}
	if offending, ok := bashCommandDeniedTokens(cmdStr, t.DenyTokens); ok {
		return "", fmt.Errorf("bash command references denied token %q", offending)
	}

	cwd, _ := params["cwd"].(string)
	cwd = strings.TrimSpace(cwd)

	timeout := t.DefaultTimeout
	if v, ok := params["timeout_seconds"]; ok {
		if secs, ok := asFloat64(v); ok && secs > 0 {
			timeout = time.Duration(secs * float64(time.Second))
		}
	}

	if t.ConfirmEachRun {
		ok, err := confirmOnTTY(fmt.Sprintf("Run bash command?\n%s\n[y/N]: ", cmdStr))
		if err != nil {
			return "", err
		}
		if !ok {
			return "aborted", nil
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", cmdStr)
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout limitedBuffer
	var stderr limitedBuffer
	stdout.Limit = t.MaxOutputBytes
	stderr.Limit = t.MaxOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else if runCtx.Err() != nil {
			return "", fmt.Errorf("bash timed out after %s", timeout)
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
		return b.String(), fmt.Errorf("bash exited with code %d", exitCode)
	}
	return b.String(), nil
}

func bashCommandDenied(cmdStr string, denyPaths []string) (string, bool) {
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" || len(denyPaths) == 0 {
		return "", false
	}
	for _, p := range denyPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if containsTokenBoundary(cmdStr, p) {
			return p, true
		}
		// Most configs will specify basenames (e.g. config.yaml). For safety,
		// also deny the basename even if a path is provided.
		if i := strings.LastIndex(p, "/"); i != -1 && i+1 < len(p) {
			base := p[i+1:]
			if base != "" && containsTokenBoundary(cmdStr, base) {
				return base, true
			}
		}
	}
	return "", false
}

func bashCommandDeniedTokens(cmdStr string, denyTokens []string) (string, bool) {
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" || len(denyTokens) == 0 {
		return "", false
	}
	for _, tok := range denyTokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if containsTokenBoundaryFold(cmdStr, tok) {
			return tok, true
		}
	}
	return "", false
}

func containsTokenBoundary(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for start := 0; ; {
		i := strings.Index(haystack[start:], needle)
		if i < 0 {
			return false
		}
		i += start
		if tokenBoundaryAt(haystack, i, len(needle)) {
			return true
		}
		start = i + 1
	}
}

func containsTokenBoundaryFold(haystack, needle string) bool {
	// ASCII-only fold, safe for typical command tokens like "curl".
	return containsTokenBoundary(strings.ToLower(haystack), strings.ToLower(needle))
}

func tokenBoundaryAt(s string, start, n int) bool {
	beforeOK := start == 0 || isBashBoundaryByte(s[start-1])
	afterIdx := start + n
	afterOK := afterIdx >= len(s) || isBashBoundaryByte(s[afterIdx])
	return beforeOK && afterOK
}

func isBashBoundaryByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	case '"', '\'', '`':
		return true
	case ';', '|', '&', '(', ')', '{', '}', '[', ']':
		return true
	case '<', '>', '=', ':', ',', '?', '#':
		return true
	case '/':
		return true
	default:
		return false
	}
}

type limitedBuffer struct {
	Limit     int
	Truncated bool
	buf       bytes.Buffer
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if w.Limit <= 0 {
		return w.buf.Write(p)
	}
	remaining := w.Limit - w.buf.Len()
	if remaining <= 0 {
		w.Truncated = true
		return len(p), nil
	}
	if len(p) <= remaining {
		return w.buf.Write(p)
	}
	_, _ = w.buf.Write(p[:remaining])
	w.Truncated = true
	return len(p), nil
}

func (w *limitedBuffer) Bytes() []byte {
	return w.buf.Bytes()
}

func asFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
