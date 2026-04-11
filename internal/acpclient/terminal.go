package acpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	methodTerminalCreate      = "terminal/create"
	methodTerminalOutput      = "terminal/output"
	methodTerminalWaitForExit = "terminal/wait_for_exit"
	methodTerminalKill        = "terminal/kill"
	methodTerminalRelease     = "terminal/release"
	defaultTerminalOutputSize = 256 * 1024
)

type terminalManager struct {
	cfg       PreparedAgentConfig
	nextID    uint64
	mu        sync.Mutex
	terminals map[string]*managedTerminal
}

type managedTerminal struct {
	id        string
	sessionID string
	cmd       *exec.Cmd
	output    *terminalOutputBuffer
	done      chan struct{}

	mu      sync.Mutex
	exited  bool
	exit    terminalExitStatus
	closeMu sync.Mutex
}

type terminalExitStatus struct {
	ExitCode *int
	Signal   *string
}

type terminalOutputBuffer struct {
	limit int

	mu        sync.Mutex
	data      []byte
	truncated bool
}

type terminalEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func newTerminalManager(cfg PreparedAgentConfig) *terminalManager {
	return &terminalManager{
		cfg:       cfg,
		terminals: map[string]*managedTerminal{},
	}
}

func (m *terminalManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	terminals := make([]*managedTerminal, 0, len(m.terminals))
	for _, term := range m.terminals {
		terminals = append(terminals, term)
	}
	m.terminals = map[string]*managedTerminal{}
	m.mu.Unlock()
	for _, term := range terminals {
		term.release()
	}
}

func (m *terminalManager) create(raw json.RawMessage) (any, *rpcError) {
	if m == nil {
		return nil, &rpcError{Code: rpcCodeMethodNotFound, Message: "method not found"}
	}
	var req struct {
		SessionID       string           `json:"sessionId"`
		Command         string           `json:"command"`
		Args            []string         `json:"args"`
		Env             []terminalEnvVar `json:"env"`
		CWD             string           `json:"cwd"`
		OutputByteLimit int              `json:"outputByteLimit"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: "invalid terminal/create request"}
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: "sessionId is required"}
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: "command is required"}
	}
	cwd, err := resolveTerminalCWD(strings.TrimSpace(req.CWD), m.cfg)
	if err != nil {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: err.Error()}
	}
	outputLimit := req.OutputByteLimit
	if outputLimit <= 0 {
		outputLimit = defaultTerminalOutputSize
	}

	cmd := exec.Command(command, cleanStrings(req.Args)...)
	cmd.Dir = cwd
	cmd.Env = mergeTerminalEnv(req.Env)
	slog.Default().Debug("acp_terminal_create_start", "command", command, "args_count", len(req.Args), "output_limit", outputLimit)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &rpcError{Code: rpcCodeInternalError, Message: err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, &rpcError{Code: rpcCodeInternalError, Message: err.Error()}
	}

	terminalID := m.nextTerminalID()
	term := &managedTerminal{
		id:        terminalID,
		sessionID: strings.TrimSpace(req.SessionID),
		cmd:       cmd,
		output:    &terminalOutputBuffer{limit: outputLimit},
		done:      make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		return nil, &rpcError{Code: rpcCodeInternalError, Message: err.Error()}
	}

	m.mu.Lock()
	m.terminals[terminalID] = term
	m.mu.Unlock()

	go term.capture(stdout)
	go term.capture(stderr)
	go term.wait()
	slog.Default().Debug("acp_terminal_create_done", "terminal_id", terminalID, "command", command)

	return map[string]any{"terminalId": terminalID}, nil
}

func (m *terminalManager) output(raw json.RawMessage) (any, *rpcError) {
	term, rpcErr := m.lookup(raw, methodTerminalOutput)
	if rpcErr != nil {
		return nil, rpcErr
	}
	output, truncated := term.output.snapshot()
	resp := map[string]any{
		"output":    output,
		"truncated": truncated,
	}
	if exit, ok := term.exitSnapshot(); ok {
		resp["exitStatus"] = terminalExitStatusMap(exit)
	}
	return resp, nil
}

func (m *terminalManager) waitForExit(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	term, rpcErr := m.lookup(raw, methodTerminalWaitForExit)
	if rpcErr != nil {
		return nil, rpcErr
	}
	startedAt := timeNow()
	slog.Default().Debug("acp_terminal_wait_start", "terminal_id", term.id)
	if err := term.waitContext(ctx); err != nil {
		return nil, &rpcError{Code: rpcCodeInternalError, Message: err.Error()}
	}
	exit, _ := term.exitSnapshot()
	slog.Default().Debug("acp_terminal_wait_done", "terminal_id", term.id, "duration_ms", sinceMillis(startedAt))
	return terminalExitStatusMap(exit), nil
}

func (m *terminalManager) kill(raw json.RawMessage) (any, *rpcError) {
	term, rpcErr := m.lookup(raw, methodTerminalKill)
	if rpcErr != nil {
		return nil, rpcErr
	}
	slog.Default().Debug("acp_terminal_kill", "terminal_id", term.id)
	if err := term.kill(); err != nil {
		return nil, &rpcError{Code: rpcCodeInternalError, Message: err.Error()}
	}
	return map[string]any{}, nil
}

func (m *terminalManager) release(raw json.RawMessage) (any, *rpcError) {
	term, terminalID, rpcErr := m.lookupWithID(raw, methodTerminalRelease)
	if rpcErr != nil {
		return nil, rpcErr
	}
	m.mu.Lock()
	delete(m.terminals, terminalID)
	m.mu.Unlock()
	if err := term.release(); err != nil {
		return nil, &rpcError{Code: rpcCodeInternalError, Message: err.Error()}
	}
	slog.Default().Debug("acp_terminal_release", "terminal_id", terminalID)
	return map[string]any{}, nil
}

func (m *terminalManager) lookup(raw json.RawMessage, method string) (*managedTerminal, *rpcError) {
	term, _, rpcErr := m.lookupWithID(raw, method)
	return term, rpcErr
}

func (m *terminalManager) lookupWithID(raw json.RawMessage, method string) (*managedTerminal, string, *rpcError) {
	if m == nil {
		return nil, "", &rpcError{Code: rpcCodeMethodNotFound, Message: "method not found"}
	}
	var req struct {
		SessionID  string `json:"sessionId"`
		TerminalID string `json:"terminalId"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, "", &rpcError{Code: rpcCodeInvalidParams, Message: fmt.Sprintf("invalid %s request", method)}
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return nil, "", &rpcError{Code: rpcCodeInvalidParams, Message: "sessionId is required"}
	}
	terminalID := strings.TrimSpace(req.TerminalID)
	if terminalID == "" {
		return nil, "", &rpcError{Code: rpcCodeInvalidParams, Message: "terminalId is required"}
	}
	m.mu.Lock()
	term, ok := m.terminals[terminalID]
	m.mu.Unlock()
	if !ok {
		return nil, "", &rpcError{Code: rpcCodeInvalidParams, Message: "unknown terminalId"}
	}
	if term.sessionID != sessionID {
		return nil, "", &rpcError{Code: rpcCodeInvalidParams, Message: "sessionId mismatch"}
	}
	return term, terminalID, nil
}

func (m *terminalManager) nextTerminalID() string {
	id := atomic.AddUint64(&m.nextID, 1)
	return fmt.Sprintf("term_%d", id)
}

func (t *managedTerminal) capture(r io.ReadCloser) {
	defer func() { _ = r.Close() }()
	_, _ = io.Copy(t.output, r)
}

func (t *managedTerminal) wait() {
	err := t.cmd.Wait()
	exit := terminalExitStatus{}
	if state := t.cmd.ProcessState; state != nil {
		if code := state.ExitCode(); code >= 0 {
			exitCode := code
			exit.ExitCode = &exitCode
		}
	}
	if err != nil && exit.ExitCode == nil {
		msg := strings.TrimSpace(err.Error())
		if msg != "" {
			exit.Signal = stringPtr(msg)
		}
	}
	t.mu.Lock()
	t.exited = true
	t.exit = exit
	t.mu.Unlock()
	if exit.ExitCode != nil {
		slog.Default().Debug("acp_terminal_exit", "terminal_id", t.id, "exit_code", *exit.ExitCode)
	} else if exit.Signal != nil {
		slog.Default().Debug("acp_terminal_exit", "terminal_id", t.id, "signal", *exit.Signal)
	} else {
		slog.Default().Debug("acp_terminal_exit", "terminal_id", t.id)
	}
	close(t.done)
}

func (t *managedTerminal) waitContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return nil
	}
}

func (t *managedTerminal) exitSnapshot() (terminalExitStatus, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.exited {
		return terminalExitStatus{}, false
	}
	return t.exit, true
}

func (t *managedTerminal) kill() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	select {
	case <-t.done:
		return nil
	default:
	}
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	err := t.cmd.Process.Kill()
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "process already finished") {
		return err
	}
	return nil
}

func (t *managedTerminal) release() error {
	if err := t.kill(); err != nil {
		return err
	}
	select {
	case <-t.done:
	default:
	}
	return nil
}

func (b *terminalOutputBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 {
		b.data = append(b.data, p...)
		return len(p), nil
	}
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.truncated = true
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return len(p), nil
}

func (b *terminalOutputBuffer) snapshot() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := bytes.ToValidUTF8(b.data, []byte("\n[non-utf8 output]\n"))
	return string(out), b.truncated
}

func resolveTerminalCWD(raw string, cfg PreparedAgentConfig) (string, error) {
	cwd := strings.TrimSpace(raw)
	if cwd == "" {
		cwd = cfg.CWD
	}
	if cwd == "" {
		return "", fmt.Errorf("cwd is required")
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(cfg.CWD, cwd)
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absCWD)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd %q is not a directory", absCWD)
	}
	for _, root := range terminalAllowedRoots(cfg) {
		if isWithinRoot(root, absCWD) {
			return absCWD, nil
		}
	}
	return "", fmt.Errorf("cwd %q is outside allowed roots", absCWD)
}

func terminalAllowedRoots(cfg PreparedAgentConfig) []string {
	seen := map[string]struct{}{}
	var roots []string
	for _, root := range append([]string{cfg.CWD}, append(cfg.ReadRoots, cfg.WriteRoots...)...) {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if _, ok := seen[absRoot]; ok {
			continue
		}
		seen[absRoot] = struct{}{}
		roots = append(roots, absRoot)
	}
	return roots
}

func mergeTerminalEnv(extra []terminalEnvVar) []string {
	env := append([]string(nil), os.Environ()...)
	if len(extra) == 0 {
		return env
	}
	index := map[string]int{}
	for i, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		index[key] = i
	}
	for _, item := range extra {
		key := strings.TrimSpace(item.Name)
		if key == "" {
			continue
		}
		entry := key + "=" + item.Value
		if i, ok := index[key]; ok {
			env[i] = entry
			continue
		}
		env = append(env, entry)
	}
	return env
}

func terminalExitStatusMap(status terminalExitStatus) map[string]any {
	out := map[string]any{
		"exitCode": nil,
		"signal":   nil,
	}
	if status.ExitCode != nil {
		out["exitCode"] = *status.ExitCode
	}
	if status.Signal != nil && strings.TrimSpace(*status.Signal) != "" {
		out["signal"] = *status.Signal
	}
	return out
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
