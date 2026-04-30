package builtin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type shellExecutionPayload struct {
	ExitCode        int    `json:"exit_code"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
}

type shellToolCommon struct {
	ToolName        string
	DefaultTimeout  time.Duration
	MaxOutputBytes  int
	Roots           pathroots.PathRoots
	DenyPaths       []string
	DenyTokens      []string
	InjectedEnvVars []string
}

type shellInvocation struct {
	Command string
	CWD     string
	Timeout time.Duration
}

type shellRunnerSpec struct {
	Program                      string
	ArgsPrefix                   []string
	BuildEnv                     func(injected []string) []string
	TokenBoundary                func(byte) bool
	MatchDeniedPath              func(cmd string, denyPaths []string) (string, bool)
	StreamOutput                 bool
	EmitChunk                    func(ctx context.Context, stream, text string)
	TimeoutExitCode              int
	ReturnObservationOnExitError bool
	ReturnObservationOnTimeout   bool
	ReturnObservationOnExecError bool
}

type shellFailureKind int

const (
	shellFailureNone shellFailureKind = iota
	shellFailureExit
	shellFailureTimeout
	shellFailureExec
)

func prepareShellInvocation(ctx context.Context, params map[string]any, common shellToolCommon, spec shellRunnerSpec) (shellInvocation, error) {
	cmdStr, _ := params["cmd"].(string)
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return shellInvocation{}, fmt.Errorf("missing required param: cmd")
	}

	var err error
	cmdStr, err = expandShellPathAliases(resolveLocalPathRoots(ctx, common.Roots), cmdStr, spec.TokenBoundary)
	if err != nil {
		return shellInvocation{}, err
	}

	if err := validateShellCommandAllowed(cmdStr, common, spec); err != nil {
		return shellInvocation{}, err
	}

	cwd, _ := params["cwd"].(string)
	cwd, err = resolveShellCWD(ctx, common.Roots, strings.TrimSpace(cwd))
	if err != nil {
		return shellInvocation{}, err
	}

	timeout := common.DefaultTimeout
	if v, ok := params["timeout_seconds"]; ok {
		if secs, ok := asFloat64(v); ok && secs > 0 {
			timeout = time.Duration(secs * float64(time.Second))
		}
	}

	return shellInvocation{
		Command: cmdStr,
		CWD:     cwd,
		Timeout: timeout,
	}, nil
}

func validateShellCommandAllowed(cmdStr string, common shellToolCommon, spec shellRunnerSpec) error {
	if spec.MatchDeniedPath != nil {
		if offending, ok := spec.MatchDeniedPath(cmdStr, common.DenyPaths); ok {
			return fmt.Errorf("%s command references denied path %q (configure via tools.%s.deny_paths)", common.ToolName, offending, common.ToolName)
		}
	}
	if offending, ok := bashCommandDeniedTokens(cmdStr, common.DenyTokens); ok {
		return fmt.Errorf("%s command references denied token %q", common.ToolName, offending)
	}
	return nil
}

func executeShellCommand(ctx context.Context, params map[string]any, common shellToolCommon, spec shellRunnerSpec) (string, error) {
	inv, err := prepareShellInvocation(ctx, params, common, spec)
	if err != nil {
		return "", err
	}
	payload, failureKind, err := runShellCommand(ctx, common, spec, inv)
	if err != nil {
		observation := formatShellObservation(payload)
		switch failureKind {
		case shellFailureExit:
			if spec.ReturnObservationOnExitError {
				return observation, err
			}
		case shellFailureTimeout:
			if spec.ReturnObservationOnTimeout {
				return observation, err
			}
		case shellFailureExec:
			if spec.ReturnObservationOnExecError {
				return observation, err
			}
		}
		return "", err
	}
	return formatShellObservation(payload), nil
}

func runShellCommand(ctx context.Context, common shellToolCommon, spec shellRunnerSpec, inv shellInvocation) (shellExecutionPayload, shellFailureKind, error) {
	runCtx, cancel := context.WithTimeout(ctx, inv.Timeout)
	defer cancel()

	args := append(append([]string(nil), spec.ArgsPrefix...), inv.Command)
	cmd := exec.CommandContext(runCtx, spec.Program, args...)
	if inv.CWD != "" {
		cmd.Dir = inv.CWD
	}
	if spec.BuildEnv != nil {
		cmd.Env = spec.BuildEnv(common.InjectedEnvVars)
	}

	var stdout limitedBuffer
	var stderr limitedBuffer
	stdout.Limit = common.MaxOutputBytes
	stderr.Limit = common.MaxOutputBytes
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return shellExecutionPayload{}, shellFailureExec, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return shellExecutionPayload{}, shellFailureExec, err
	}
	if err := cmd.Start(); err != nil {
		return shellExecutionPayload{}, shellFailureExec, err
	}

	var streamWG sync.WaitGroup
	var stdoutReadErr error
	var stderrReadErr error
	streamWG.Add(2)
	go func() {
		defer streamWG.Done()
		stdoutReadErr = readShellPipe(runCtx, "stdout", stdoutPipe, &stdout, streamEmitter(spec, ctx))
	}()
	go func() {
		defer streamWG.Done()
		stderrReadErr = readShellPipe(runCtx, "stderr", stderrPipe, &stderr, streamEmitter(spec, ctx))
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
	failureKind := shellFailureNone
	if err != nil {
		switch {
		case runCtx.Err() != nil:
			exitCode = spec.TimeoutExitCode
			failureKind = shellFailureTimeout
			err = fmt.Errorf("%s timed out after %s", common.ToolName, inv.Timeout)
		case isExitError(err):
			exitCode = exitCodeFromError(err)
			failureKind = shellFailureExit
		default:
			exitCode = -1
			failureKind = shellFailureExec
		}
	}

	payload := shellExecutionPayload{
		ExitCode:        exitCode,
		StdoutTruncated: stdout.Truncated,
		StderrTruncated: stderr.Truncated,
		Stdout:          string(bytes.ToValidUTF8(stdout.Bytes(), []byte("\n[non-utf8 output]\n"))),
		Stderr:          string(bytes.ToValidUTF8(stderr.Bytes(), []byte("\n[non-utf8 output]\n"))),
	}
	if err == nil {
		return payload, shellFailureNone, nil
	}
	if failureKind == shellFailureExit {
		err = fmt.Errorf("%s exited with code %d", common.ToolName, exitCode)
	}
	return payload, failureKind, err
}

func readShellPipe(ctx context.Context, stream string, r io.Reader, dst *limitedBuffer, emit func(stream, text string)) error {
	if r == nil || dst == nil {
		return nil
	}
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			_, _ = dst.Write(chunk)
			if emit != nil {
				text := string(bytes.ToValidUTF8(chunk, []byte("\n[non-utf8 output]\n")))
				if strings.TrimSpace(text) != "" {
					emit(stream, text)
				}
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			return nil
		}
		if isBenignCommandStreamReadError(err) {
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

func streamEmitter(spec shellRunnerSpec, ctx context.Context) func(stream, text string) {
	if !spec.StreamOutput || spec.EmitChunk == nil {
		return nil
	}
	return func(stream, text string) {
		spec.EmitChunk(ctx, stream, text)
	}
}

func resolveShellCWD(ctx context.Context, roots pathroots.PathRoots, raw string) (string, error) {
	roots = resolveLocalPathRoots(ctx, roots)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if workspaceDir := strings.TrimSpace(roots.WorkspaceDir); workspaceDir != "" {
			return workspaceDir, nil
		}
		return "", nil
	}
	raw = pathutil.ExpandHomePath(raw)
	alias, rest := detectPathAlias(raw)
	if alias != "" {
		return resolveAliasedPath(roots, alias, rest, false)
	}
	if filepath.IsAbs(raw) {
		return filepath.Abs(filepath.Clean(raw))
	}
	if workspaceDir := strings.TrimSpace(roots.WorkspaceDir); workspaceDir != "" {
		return filepath.Abs(filepath.Join(workspaceDir, raw))
	}
	return pathutil.ExpandHomePath(raw), nil
}

func expandShellPathAliases(roots pathroots.PathRoots, cmd string, isBoundary func(byte) bool) (string, error) {
	var err error
	cmd, err = replaceAliasTokenInCommand(cmd, "workspace_dir", strings.TrimSpace(roots.WorkspaceDir), isBoundary)
	if err != nil {
		return "", err
	}
	cmd, err = replaceAliasTokenInCommand(cmd, "file_cache_dir", strings.TrimSpace(roots.FileCacheDir), isBoundary)
	if err != nil {
		return "", err
	}
	cmd, err = replaceAliasTokenInCommand(cmd, "file_state_dir", strings.TrimSpace(roots.FileStateDir), isBoundary)
	if err != nil {
		return "", err
	}
	return cmd, nil
}

func formatShellObservation(payload shellExecutionPayload) string {
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
