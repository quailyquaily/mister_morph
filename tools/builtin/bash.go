package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/tools"
)

type BashTool struct {
	Enabled         bool
	DefaultTimeout  time.Duration
	MaxOutputBytes  int
	BaseDirs        []string
	DenyPaths       []string
	DenyTokens      []string
	InjectedEnvVars []string
}

type bashExecutionPayload struct {
	ExitCode        int    `json:"exit_code"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
}

func NewBashTool(enabled bool, defaultTimeout time.Duration, maxOutputBytes int, baseDirs ...string) *BashTool {
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = 256 * 1024
	}
	return &BashTool{
		Enabled:        enabled,
		DefaultTimeout: defaultTimeout,
		MaxOutputBytes: maxOutputBytes,
		BaseDirs:       normalizeBaseDirs(baseDirs),
	}
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	return "Runs a bash command and returns stdout/stderr." +
		"For the `cmd` and `cwd`, supports path aliases file_cache_dir and file_state_dir."
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
				"description": "Optional timeout in seconds.",
			},
			"run_in_subtask": map[string]any{
				"type":        "boolean",
				"description": "Optional. If true, run this command inside a child subtask and return the child subtask envelope as JSON.",
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
	var err error
	cmdStr, err = t.expandPathAliasesInCommand(cmdStr)
	if err != nil {
		return "", err
	}

	if offending, ok := bashCommandDenied(cmdStr, t.DenyPaths); ok {
		return "", fmt.Errorf("bash command references denied path %q (configure via tools.bash.deny_paths)", offending)
	}
	if offending, ok := bashCommandDeniedTokens(cmdStr, t.DenyTokens); ok {
		return "", fmt.Errorf("bash command references denied token %q", offending)
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
	runInSubtask, _ := asBool(params["run_in_subtask"])
	if runInSubtask && agent.SubtaskDepthFromContext(ctx) == 0 {
		return t.executeInSubtask(ctx, cmdStr, cwd, timeout)
	}

	payload, err := t.runCommand(ctx, cmdStr, cwd, timeout)
	observation := formatBashObservation(payload)
	if err != nil {
		return observation, err
	}
	return observation, nil
}

func (t *BashTool) executeInSubtask(ctx context.Context, cmdStr string, cwd string, timeout time.Duration) (string, error) {
	runner, ok := agent.SubtaskRunnerFromContext(ctx)
	if !ok {
		taskID, runCtx, _ := agent.PrepareSubtaskContext(ctx, nil)
		payload, err := t.runCommand(runCtx, cmdStr, cwd, timeout)
		result := buildBashSubtaskResult(taskID, payload, err)
		return marshalBashSubtaskResult(result, err)
	}
	req := agent.SubtaskRequest{
		OutputSchema:   "subtask.bash.result.v1",
		ObserveProfile: agent.ObserveProfileLongShell,
		RunFunc: func(runCtx context.Context) (*agent.SubtaskResult, error) {
			payload, err := t.runCommand(runCtx, cmdStr, cwd, timeout)
			return buildBashSubtaskResult("", payload, err), nil
		},
	}
	result, err := runner.RunSubtask(ctx, req)
	if err != nil {
		return "", err
	}
	return marshalBashSubtaskResult(result, bashSubtaskError(result))
}

func (t *BashTool) runCommand(ctx context.Context, cmdStr string, cwd string, timeout time.Duration) (bashExecutionPayload, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", cmdStr)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = bashToolEnv(t.InjectedEnvVars)

	var stdout limitedBuffer
	var stderr limitedBuffer
	stdout.Limit = t.MaxOutputBytes
	stderr.Limit = t.MaxOutputBytes
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return bashExecutionPayload{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return bashExecutionPayload{}, err
	}
	if err := cmd.Start(); err != nil {
		return bashExecutionPayload{}, err
	}

	var streamWG sync.WaitGroup
	var stdoutReadErr error
	var stderrReadErr error
	streamWG.Add(2)
	go func() {
		defer streamWG.Done()
		stdoutReadErr = t.captureCommandStream(runCtx, "stdout", stdoutPipe, &stdout)
	}()
	go func() {
		defer streamWG.Done()
		stderrReadErr = t.captureCommandStream(runCtx, "stderr", stderrPipe, &stderr)
	}()

	err = cmd.Wait()
	streamWG.Wait()
	if err == nil {
		if stdoutReadErr != nil {
			err = stdoutReadErr
		} else if stderrReadErr != nil {
			err = stderrReadErr
		}
	}
	exitCode := 0
	if err != nil {
		switch {
		case isExitError(err):
			exitCode = exitCodeFromError(err)
		case runCtx.Err() != nil:
			exitCode = 124
			err = fmt.Errorf("bash timed out after %s", timeout)
		default:
			exitCode = -1
		}
	}

	payload := bashExecutionPayload{
		ExitCode:        exitCode,
		StdoutTruncated: stdout.Truncated,
		StderrTruncated: stderr.Truncated,
		Stdout:          string(bytes.ToValidUTF8(stdout.Bytes(), []byte("\n[non-utf8 output]\n"))),
		Stderr:          string(bytes.ToValidUTF8(stderr.Bytes(), []byte("\n[non-utf8 output]\n"))),
	}

	if err == nil {
		return payload, nil
	}
	if exitCode > 0 && !strings.Contains(err.Error(), "timed out after") {
		err = fmt.Errorf("bash exited with code %d", exitCode)
	}
	return payload, err
}

func (t *BashTool) captureCommandStream(ctx context.Context, stream string, r io.Reader, dst *limitedBuffer) error {
	if r == nil || dst == nil {
		return nil
	}
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			_, _ = dst.Write(chunk)
			text := string(bytes.ToValidUTF8(chunk, []byte("\n[non-utf8 output]\n")))
			if strings.TrimSpace(text) != "" {
				agent.EmitEvent(ctx, nil, agent.Event{
					Kind:     agent.EventKindToolOutput,
					ToolName: t.Name(),
					Profile:  string(agent.ObserveProfileLongShell),
					Stream:   stream,
					Text:     text,
					Status:   "running",
				})
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		default:
			return err
		}
	}
}

func formatBashObservation(payload bashExecutionPayload) string {
	var out strings.Builder
	fmt.Fprintf(&out, "exit_code: %d\n", payload.ExitCode)
	fmt.Fprintf(&out, "stdout_truncated: %t\n", payload.StdoutTruncated)
	fmt.Fprintf(&out, "stderr_truncated: %t\n", payload.StderrTruncated)
	out.WriteString("stdout:\n")
	out.WriteString(payload.Stdout)
	out.WriteString("\n\nstderr:\n")
	out.WriteString(payload.Stderr)
	return out.String()
}

func buildBashSubtaskResult(taskID string, payload bashExecutionPayload, execErr error) *agent.SubtaskResult {
	result := &agent.SubtaskResult{
		TaskID:       strings.TrimSpace(taskID),
		Status:       agent.SubtaskStatusDone,
		Summary:      fmt.Sprintf("bash exited with code %d", payload.ExitCode),
		OutputKind:   agent.SubtaskOutputKindJSON,
		OutputSchema: "subtask.bash.result.v1",
		Output:       payload,
		Error:        "",
	}
	if execErr != nil {
		result.Status = agent.SubtaskStatusFailed
		result.Summary = strings.TrimSpace(execErr.Error())
		result.Error = strings.TrimSpace(execErr.Error())
	}
	return result
}

func marshalBashSubtaskResult(result *agent.SubtaskResult, execErr error) (string, error) {
	if result == nil {
		return "", fmt.Errorf("bash subtask returned nil result")
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	if execErr != nil {
		return string(b), tools.PreserveObservationError(execErr)
	}
	return string(b), nil
}

func bashSubtaskError(result *agent.SubtaskResult) error {
	if result == nil || strings.TrimSpace(result.Status) != agent.SubtaskStatusFailed {
		return nil
	}
	if msg := strings.TrimSpace(result.Error); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	if msg := strings.TrimSpace(result.Summary); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("bash subtask failed")
}

func isExitError(err error) bool {
	_, ok := err.(*exec.ExitError)
	return ok
}

func exitCodeFromError(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

func bashToolEnv(injected []string) []string {
	pathValue := strings.TrimSpace(os.Getenv("PATH"))
	if pathValue == "" {
		pathValue = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	env := []string{"PATH=" + pathValue}
	seen := map[string]bool{"PATH": true}
	for _, key := range []string{
		"HOME",
		"LANG",
		"LC_ALL",
		"LC_CTYPE",
		"TERM",
		"TZ",
		"TMPDIR",
		"USER",
		"LOGNAME",
		"SHELL",
		"XDG_CONFIG_HOME",
		"XDG_CACHE_HOME",
		"XDG_DATA_HOME",
		"XDG_RUNTIME_DIR",
		"SSL_CERT_FILE",
		"SSL_CERT_DIR",
	} {
		seen[key] = true
		value, ok := os.LookupEnv(key)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		env = append(env, key+"="+value)
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

func normalizeInjectedEnvVarName(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ""
	}
	for i, r := range key {
		switch {
		case r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			if i == 0 {
				continue
			}
		case i > 0 && r >= '0' && r <= '9':
			continue
		default:
			return ""
		}
	}
	return key
}

func (t *BashTool) resolveCWD(raw string) (string, error) {
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

func (t *BashTool) expandPathAliasesInCommand(cmd string) (string, error) {
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

func replaceAliasTokenInCommand(cmd, alias, baseDir string) (string, error) {
	cmd = strings.TrimSpace(cmd)
	alias = strings.TrimSpace(alias)
	if cmd == "" || alias == "" {
		return cmd, nil
	}
	lower := strings.ToLower(cmd)
	needle := strings.ToLower(alias)

	last := 0
	start := 0
	var b strings.Builder
	matched := false
	for {
		i := strings.Index(lower[start:], needle)
		if i < 0 {
			break
		}
		i += start
		if !tokenBoundaryAt(lower, i, len(needle)) {
			start = i + 1
			continue
		}
		if strings.TrimSpace(baseDir) == "" {
			return "", fmt.Errorf("base dir %s is not configured", alias)
		}
		matched = true
		b.WriteString(cmd[last:i])
		b.WriteString(baseDir)
		last = i + len(needle)
		start = last
	}
	if !matched {
		return cmd, nil
	}
	b.WriteString(cmd[last:])
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
