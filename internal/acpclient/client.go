package acpclient

import (
	"bufio"
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
	"time"
)

const (
	protocolVersion         = 1
	methodAuthenticate      = "authenticate"
	methodInitialize        = "initialize"
	methodSessionNew        = "session/new"
	methodSessionPrompt     = "session/prompt"
	methodSessionCancel     = "session/cancel"
	methodSessionSetConfig  = "session/set_config_option"
	methodSessionUpdate     = "session/update"
	methodRequestPerm       = "session/request_permission"
	methodReadTextFile      = "fs/read_text_file"
	methodWriteTextFile     = "fs/write_text_file"
	jsonRPCVersion          = "2.0"
	rpcCodeMethodNotFound   = -32601
	rpcCodeInvalidParams    = -32602
	rpcCodeInternalError    = -32603
	defaultClientName       = "mistermorph"
	defaultClientTitle      = "MisterMorph"
	defaultClientVersion    = "1.0.0"
	permissionAllowOnce     = "allow_once"
	permissionAllowAlways   = "allow_always"
	permissionRejectOnce    = "reject_once"
	permissionOutcomeSel    = "selected"
	permissionOutcomeCancel = "cancelled"
	maxRPCStderrBytes       = 64 * 1024
	maxReadTextFileBytes    = 1024 * 1024
)

type EventKind string

const (
	EventKindAgentMessageChunk EventKind = "agent_message_chunk"
	EventKindToolCallStart     EventKind = "tool_call_start"
	EventKindToolCallUpdate    EventKind = "tool_call_update"
	EventKindToolCallDone      EventKind = "tool_call_done"
)

type Event struct {
	Kind       EventKind
	ToolCallID string
	Title      string
	ToolKind   string
	Status     string
	Text       string
}

type Observer interface {
	HandleACPEvent(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (fn ObserverFunc) HandleACPEvent(ctx context.Context, event Event) {
	if fn != nil {
		fn(ctx, event)
	}
}

type RunRequest struct {
	Prompt   string
	Observer Observer
}

type RunResult struct {
	SessionID  string
	StopReason string
	Output     string
}

type initResult struct {
	AuthMethods []authMethod `json:"authMethods"`
}

type newSessionResult struct {
	SessionID     string              `json:"sessionId"`
	ConfigOptions []sessionConfigInfo `json:"configOptions"`
}

type authMethod struct {
	ID   string       `json:"id"`
	Type string       `json:"type,omitempty"`
	Vars []authEnvVar `json:"vars,omitempty"`
}

type authEnvVar struct {
	Name string `json:"name"`
}

type sessionConfigInfo struct {
	ID string `json:"id"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "json-rpc error"
	}
	return fmt.Sprintf("%s (code=%d)", msg, e.Code)
}

type pendingResponse struct {
	result json.RawMessage
	err    *rpcError
}

type requestHandler func(context.Context, rpcMessage) (any, *rpcError)
type notificationHandler func(context.Context, rpcMessage)
type connFactory func(context.Context, PreparedAgentConfig) (*rpcConn, error)

type rpcConn struct {
	ctx    context.Context
	cancel context.CancelFunc

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	writeMu sync.Mutex
	enc     *json.Encoder

	nextID  int64
	pendMu  sync.Mutex
	pending map[string]chan pendingResponse

	reqHandler  requestHandler
	noteHandler notificationHandler

	closeOnce sync.Once
	closeErr  error
	done      chan struct{}

	stderrMu  sync.Mutex
	stderrBuf cappedTailBuffer
}

func RunPrompt(ctx context.Context, cfg PreparedAgentConfig, req RunRequest) (RunResult, error) {
	return runPromptWithFactory(ctx, cfg, req, newStdioConn)
}

func runPromptWithFactory(ctx context.Context, cfg PreparedAgentConfig, req RunRequest, factory connFactory) (RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return RunResult{}, fmt.Errorf("empty acp prompt")
	}
	if factory == nil {
		factory = newStdioConn
	}
	conn, err := factory(ctx, cfg)
	if err != nil {
		return RunResult{}, err
	}
	defer func() { _ = conn.Close() }()
	terminals := newTerminalManager(cfg)
	defer terminals.Close()

	initResp, err := conn.initialize(ctx)
	if err != nil {
		return RunResult{}, err
	}
	if err := conn.authenticate(ctx, cfg, initResp.AuthMethods); err != nil {
		return RunResult{}, err
	}

	session, err := conn.newSession(ctx, cfg)
	if err != nil {
		return RunResult{}, err
	}
	if err := conn.applySessionOptions(ctx, session.SessionID, cfg.SessionOptionsMeta, session.ConfigOptions); err != nil {
		return RunResult{}, err
	}

	state := newRunState(req.Observer)
	conn.reqHandler = func(callCtx context.Context, msg rpcMessage) (any, *rpcError) {
		return handleIncomingRequest(callCtx, cfg, session.SessionID, terminals, msg)
	}
	conn.noteHandler = func(noteCtx context.Context, msg rpcMessage) {
		handleNotification(noteCtx, session.SessionID, msg, state)
	}

	stopCancelWatcher := make(chan struct{})
	cancelDone := make(chan struct{})
	go func() {
		defer close(cancelDone)
		select {
		case <-ctx.Done():
			_ = conn.notify(context.Background(), methodSessionCancel, map[string]any{
				"sessionId": session.SessionID,
			})
		case <-stopCancelWatcher:
		case <-conn.done:
		}
	}()
	defer func() {
		close(stopCancelWatcher)
		<-cancelDone
	}()

	stopReason, err := conn.prompt(ctx, session.SessionID, req.Prompt)
	if err != nil {
		return RunResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RunResult{}, err
	}
	slog.Default().Debug("acp_prompt_done", "agent", cfg.Name, "stop_reason", stopReason)

	return RunResult{
		SessionID:  session.SessionID,
		StopReason: stopReason,
		Output:     state.output(),
	}, nil
}

func newStdioConn(parent context.Context, cfg PreparedAgentConfig) (*rpcConn, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = cfg.CWD
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		cmd.Env = append(cmd.Env, key+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp stdio stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp stdio stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("acp stdio stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("acp stdio start: %w", err)
	}

	conn := &rpcConn{
		ctx:     parent,
		cancel:  cancel,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		enc:     json.NewEncoder(stdin),
		pending: map[string]chan pendingResponse{},
		done:    make(chan struct{}),
		stderrBuf: cappedTailBuffer{
			limit: maxRPCStderrBytes,
		},
	}
	go conn.readLoop()
	go conn.drainStderr()
	return conn, nil
}

func (c *rpcConn) initialize(ctx context.Context) (initResult, error) {
	startedAt := timeNow()
	result, err := c.call(ctx, methodInitialize, map[string]any{
		"protocolVersion": protocolVersion,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{
				"readTextFile":  true,
				"writeTextFile": true,
			},
			"terminal": true,
		},
		"clientInfo": map[string]any{
			"name":    defaultClientName,
			"title":   defaultClientTitle,
			"version": defaultClientVersion,
		},
	})
	if err != nil {
		return initResult{}, fmt.Errorf("acp initialize: %w", err)
	}
	var resp struct {
		ProtocolVersion int          `json:"protocolVersion"`
		AuthMethods     []authMethod `json:"authMethods"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return initResult{}, fmt.Errorf("decode acp initialize response: %w", err)
	}
	if resp.ProtocolVersion != protocolVersion {
		return initResult{}, fmt.Errorf("unsupported acp protocol version %d", resp.ProtocolVersion)
	}
	slog.Default().Debug("acp_initialize_done", "duration_ms", sinceMillis(startedAt), "auth_methods", len(resp.AuthMethods))
	return initResult{AuthMethods: resp.AuthMethods}, nil
}

func (c *rpcConn) authenticate(ctx context.Context, cfg PreparedAgentConfig, methods []authMethod) error {
	methodID := selectAuthMethod(cfg, methods)
	if methodID == "" {
		slog.Default().Debug("acp_authenticate_skipped", "agent", cfg.Name)
		return nil
	}
	startedAt := timeNow()
	_, err := c.call(ctx, methodAuthenticate, map[string]any{
		"methodId": methodID,
	})
	if err != nil {
		return fmt.Errorf("acp authenticate(%s): %w", methodID, err)
	}
	slog.Default().Debug("acp_authenticate_done", "agent", cfg.Name, "method_id", methodID, "duration_ms", sinceMillis(startedAt))
	return nil
}

func (c *rpcConn) newSession(ctx context.Context, cfg PreparedAgentConfig) (newSessionResult, error) {
	startedAt := timeNow()
	params := map[string]any{
		"cwd":        cfg.CWD,
		"mcpServers": []any{},
	}
	if len(cfg.SessionOptionsMeta) > 0 {
		params["_meta"] = cloneMeta(cfg.SessionOptionsMeta)
	}
	result, err := c.call(ctx, methodSessionNew, params)
	if err != nil {
		return newSessionResult{}, fmt.Errorf("acp session/new: %w", err)
	}
	var resp newSessionResult
	if err := json.Unmarshal(result, &resp); err != nil {
		return newSessionResult{}, fmt.Errorf("decode acp session/new response: %w", err)
	}
	resp.SessionID = strings.TrimSpace(resp.SessionID)
	if resp.SessionID == "" {
		return newSessionResult{}, fmt.Errorf("acp session/new returned empty sessionId")
	}
	slog.Default().Debug("acp_session_new_done", "agent", cfg.Name, "duration_ms", sinceMillis(startedAt), "config_options", len(resp.ConfigOptions))
	return resp, nil
}

func (c *rpcConn) applySessionOptions(ctx context.Context, sessionID string, options map[string]any, configOptions []sessionConfigInfo) error {
	if len(options) == 0 {
		return nil
	}
	allowed := map[string]struct{}{}
	for _, option := range configOptions {
		id := strings.TrimSpace(option.ID)
		if id == "" {
			continue
		}
		allowed[id] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil
	}
	for configID, value := range options {
		configID = strings.TrimSpace(configID)
		if configID == "" {
			continue
		}
		if _, ok := allowed[configID]; !ok {
			continue
		}
		startedAt := timeNow()
		params := map[string]any{
			"sessionId": sessionID,
			"configId":  configID,
			"value":     value,
		}
		if _, ok := value.(bool); ok {
			params["type"] = "boolean"
		}
		if _, err := c.call(ctx, methodSessionSetConfig, params); err != nil {
			return fmt.Errorf("acp session/set_config_option(%s): %w", configID, err)
		}
		slog.Default().Debug("acp_session_config_done", "config_id", configID, "duration_ms", sinceMillis(startedAt))
	}
	return nil
}

func selectAuthMethod(cfg PreparedAgentConfig, methods []authMethod) string {
	if len(methods) == 0 {
		return ""
	}
	byID := make(map[string]authMethod, len(methods))
	for _, method := range methods {
		id := normalizeAuthMethodID(method.ID)
		if id == "" {
			continue
		}
		byID[id] = method
	}
	if _, ok := byID["codex-api-key"]; ok && hasEffectiveEnv(cfg, "CODEX_API_KEY") {
		return "codex-api-key"
	}
	if _, ok := byID["openai-api-key"]; ok && hasEffectiveEnv(cfg, "OPENAI_API_KEY") {
		return "openai-api-key"
	}
	if _, ok := byID["chatgpt"]; ok {
		return "chatgpt"
	}
	for _, method := range methods {
		id := normalizeAuthMethodID(method.ID)
		if id == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(method.Type), "env_var") && authVarsAvailable(cfg, method.Vars) {
			return id
		}
	}
	return ""
}

func normalizeAuthMethodID(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func authVarsAvailable(cfg PreparedAgentConfig, vars []authEnvVar) bool {
	if len(vars) == 0 {
		return false
	}
	for _, variable := range vars {
		if !hasEffectiveEnv(cfg, variable.Name) {
			return false
		}
	}
	return true
}

func hasEffectiveEnv(cfg PreparedAgentConfig, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if value := strings.TrimSpace(cfg.Env[name]); value != "" {
		return true
	}
	value, ok := os.LookupEnv(name)
	return ok && strings.TrimSpace(value) != ""
}

func (c *rpcConn) prompt(ctx context.Context, sessionID string, prompt string) (string, error) {
	startedAt := timeNow()
	slog.Default().Debug("acp_prompt_start", "prompt_len", len(strings.TrimSpace(prompt)))
	result, err := c.call(ctx, methodSessionPrompt, map[string]any{
		"sessionId": sessionID,
		"prompt": []map[string]any{
			{
				"type": "text",
				"text": prompt,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("acp session/prompt: %w", err)
	}
	var resp struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("decode acp session/prompt response: %w", err)
	}
	stopReason := strings.TrimSpace(resp.StopReason)
	if stopReason == "" {
		return "", fmt.Errorf("acp session/prompt returned empty stopReason")
	}
	slog.Default().Debug("acp_prompt_response", "duration_ms", sinceMillis(startedAt), "stop_reason", stopReason)
	return stopReason, nil
}

func (c *rpcConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	id := atomic.AddInt64(&c.nextID, 1)
	idRaw, _ := json.Marshal(id)
	key := string(idRaw)
	respCh := make(chan pendingResponse, 1)

	c.pendMu.Lock()
	c.pending[key] = respCh
	c.pendMu.Unlock()

	if err := c.send(rpcMessage{
		JSONRPC: jsonRPCVersion,
		ID:      idRaw,
		Method:  method,
		Params:  mustMarshalRaw(params),
	}); err != nil {
		c.deletePending(key)
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.deletePending(key)
		return nil, ctx.Err()
	case <-c.done:
		c.deletePending(key)
		if stderr := strings.TrimSpace(c.stderrString()); stderr != "" {
			return nil, fmt.Errorf("acp connection closed: %s", stderr)
		}
		return nil, io.EOF
	case resp := <-respCh:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if resp.err != nil {
			return nil, resp.err
		}
		return resp.result, nil
	}
}

func (c *rpcConn) notify(ctx context.Context, method string, params any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return io.EOF
	default:
	}
	return c.send(rpcMessage{
		JSONRPC: jsonRPCVersion,
		Method:  method,
		Params:  mustMarshalRaw(params),
	})
}

func (c *rpcConn) send(msg rpcMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.enc.Encode(msg)
}

func (c *rpcConn) readLoop() {
	defer close(c.done)
	dec := json.NewDecoder(c.stdout)
	for {
		var msg rpcMessage
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				return
			}
			c.failPending(fmt.Errorf("acp decode: %w", err))
			return
		}
		switch {
		case strings.TrimSpace(msg.Method) != "" && len(msg.ID) > 0:
			result, rpcErr := c.handleRequest(msg)
			response := rpcMessage{
				JSONRPC: jsonRPCVersion,
				ID:      append(json.RawMessage(nil), msg.ID...),
			}
			if rpcErr != nil {
				response.Error = rpcErr
			} else {
				response.Result = mustMarshalRaw(result)
			}
			_ = c.send(response)
		case strings.TrimSpace(msg.Method) != "":
			c.handleNotification(msg)
		default:
			c.handleResponse(msg)
		}
	}
}

func (c *rpcConn) handleRequest(msg rpcMessage) (any, *rpcError) {
	if c.reqHandler == nil {
		return nil, &rpcError{Code: rpcCodeMethodNotFound, Message: "method not found"}
	}
	return c.reqHandler(c.ctx, msg)
}

func (c *rpcConn) handleNotification(msg rpcMessage) {
	if c.noteHandler == nil {
		return
	}
	c.noteHandler(c.ctx, msg)
}

func (c *rpcConn) handleResponse(msg rpcMessage) {
	key := string(msg.ID)
	c.pendMu.Lock()
	ch, ok := c.pending[key]
	if ok {
		delete(c.pending, key)
	}
	c.pendMu.Unlock()
	if !ok {
		return
	}
	ch <- pendingResponse{result: msg.Result, err: msg.Error}
}

func (c *rpcConn) deletePending(key string) {
	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	delete(c.pending, key)
}

func (c *rpcConn) failPending(err error) {
	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	if len(c.pending) == 0 {
		return
	}
	rpcErr := &rpcError{Code: rpcCodeInternalError, Message: strings.TrimSpace(err.Error())}
	for key, ch := range c.pending {
		ch <- pendingResponse{err: rpcErr}
		delete(c.pending, key)
	}
}

func (c *rpcConn) drainStderr() {
	if c.stderr == nil {
		return
	}
	_, _ = io.Copy(&lockedTailWriter{mu: &c.stderrMu, buf: &c.stderrBuf}, c.stderr)
}

func (c *rpcConn) stderrString() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	return c.stderrBuf.String()
}

func (c *rpcConn) Close() error {
	c.closeOnce.Do(func() {
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		if c.cancel != nil {
			c.cancel()
		}
		if c.cmd != nil {
			c.closeErr = c.cmd.Wait()
		}
	})
	return c.closeErr
}

type runState struct {
	observer Observer
	mu       sync.Mutex
	outputs  []string
	toolInfo map[string]Event
}

func newRunState(observer Observer) *runState {
	return &runState{
		observer: observer,
		toolInfo: map[string]Event{},
	}
}

func (s *runState) emit(ctx context.Context, event Event) {
	if s == nil {
		return
	}
	if event.Kind == EventKindAgentMessageChunk && event.Text != "" {
		s.mu.Lock()
		s.outputs = append(s.outputs, event.Text)
		s.mu.Unlock()
	}
	if strings.TrimSpace(event.ToolCallID) != "" {
		s.mu.Lock()
		prev := s.toolInfo[event.ToolCallID]
		if strings.TrimSpace(event.Title) == "" {
			event.Title = prev.Title
		}
		if strings.TrimSpace(event.ToolKind) == "" {
			event.ToolKind = prev.ToolKind
		}
		if strings.TrimSpace(event.Status) == "" {
			event.Status = prev.Status
		}
		s.toolInfo[event.ToolCallID] = event
		s.mu.Unlock()
	}
	if s.observer != nil {
		s.observer.HandleACPEvent(ctx, event)
	}
}

func (s *runState) output() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.outputs, "")
}

func handleIncomingRequest(ctx context.Context, cfg PreparedAgentConfig, sessionID string, terminals *terminalManager, msg rpcMessage) (any, *rpcError) {
	slog.Default().Debug("acp_request_in", "method", strings.TrimSpace(msg.Method))
	switch strings.TrimSpace(msg.Method) {
	case methodRequestPerm:
		return handlePermissionRequest(ctx, msg.Params)
	case methodReadTextFile:
		return handleReadTextFile(sessionID, cfg, msg.Params)
	case methodWriteTextFile:
		return handleWriteTextFile(sessionID, cfg, msg.Params)
	case methodTerminalCreate:
		return terminals.create(msg.Params)
	case methodTerminalOutput:
		return terminals.output(msg.Params)
	case methodTerminalWaitForExit:
		return terminals.waitForExit(ctx, msg.Params)
	case methodTerminalKill:
		return terminals.kill(msg.Params)
	case methodTerminalRelease:
		return terminals.release(msg.Params)
	default:
		return nil, &rpcError{Code: rpcCodeMethodNotFound, Message: "method not found"}
	}
}

func handleNotification(ctx context.Context, sessionID string, msg rpcMessage, state *runState) {
	if strings.TrimSpace(msg.Method) != methodSessionUpdate {
		return
	}
	var note struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(msg.Params, &note); err != nil {
		return
	}
	if strings.TrimSpace(note.SessionID) != strings.TrimSpace(sessionID) {
		return
	}
	var kind struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	if err := json.Unmarshal(note.Update, &kind); err != nil {
		return
	}
	slog.Default().Debug("acp_update_in", "kind", strings.TrimSpace(kind.SessionUpdate))
	switch strings.TrimSpace(kind.SessionUpdate) {
	case string(EventKindAgentMessageChunk):
		var update struct {
			Content any `json:"content"`
		}
		if err := json.Unmarshal(note.Update, &update); err != nil {
			return
		}
		text := extractText(update.Content)
		if text == "" {
			return
		}
		state.emit(ctx, Event{
			Kind: EventKindAgentMessageChunk,
			Text: text,
		})
	case "tool_call":
		var update struct {
			ToolCallID string `json:"toolCallId"`
			Title      string `json:"title"`
			Kind       string `json:"kind"`
			Status     string `json:"status"`
			Content    any    `json:"content"`
		}
		if err := json.Unmarshal(note.Update, &update); err != nil {
			return
		}
		text := extractText(update.Content)
		state.emit(ctx, Event{
			Kind:       EventKindToolCallStart,
			ToolCallID: strings.TrimSpace(update.ToolCallID),
			Title:      strings.TrimSpace(update.Title),
			ToolKind:   strings.TrimSpace(update.Kind),
			Status:     strings.TrimSpace(update.Status),
			Text:       text,
		})
	case "tool_call_update":
		var update struct {
			ToolCallID string `json:"toolCallId"`
			Title      string `json:"title"`
			Kind       string `json:"kind"`
			Status     string `json:"status"`
			Content    any    `json:"content"`
		}
		if err := json.Unmarshal(note.Update, &update); err != nil {
			return
		}
		eventKind := EventKindToolCallUpdate
		status := strings.TrimSpace(update.Status)
		switch status {
		case "completed", "failed":
			eventKind = EventKindToolCallDone
		}
		state.emit(ctx, Event{
			Kind:       eventKind,
			ToolCallID: strings.TrimSpace(update.ToolCallID),
			Title:      strings.TrimSpace(update.Title),
			ToolKind:   strings.TrimSpace(update.Kind),
			Status:     status,
			Text:       extractText(update.Content),
		})
	}
}

func handlePermissionRequest(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var req struct {
		ToolCall struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: "invalid permission request"}
	}
	if ctx != nil && ctx.Err() != nil {
		return map[string]any{"outcome": permissionOutcomeCancel}, nil
	}
	if optionID, ok := choosePermissionOption(req.ToolCall.Kind, req.ToolCall.Title, req.Options); ok {
		return map[string]any{
			"outcome":  permissionOutcomeSel,
			"optionId": optionID,
		}, nil
	}
	return map[string]any{"outcome": permissionOutcomeCancel}, nil
}

func choosePermissionOption(toolKind string, title string, options []struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
}) (string, bool) {
	if len(options) == 0 {
		return "", false
	}
	for _, wanted := range []string{permissionAllowOnce, permissionAllowAlways, permissionRejectOnce} {
		for _, option := range options {
			if strings.TrimSpace(option.Kind) != wanted || strings.TrimSpace(option.OptionID) == "" {
				continue
			}
			return strings.TrimSpace(option.OptionID), true
		}
	}
	return "", false
}

func handleReadTextFile(sessionID string, cfg PreparedAgentConfig, raw json.RawMessage) (any, *rpcError) {
	var req struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: "invalid fs/read_text_file request"}
	}
	if strings.TrimSpace(req.SessionID) != strings.TrimSpace(sessionID) {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: "sessionId mismatch"}
	}
	path, err := resolveAllowedPath(req.Path, cfg.ReadRoots)
	if err != nil {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: err.Error()}
	}
	content, err := readTextFileContent(path, req.Line, req.Limit)
	if err != nil {
		return nil, &rpcError{Code: rpcCodeInternalError, Message: err.Error()}
	}
	return map[string]any{
		"content": content,
	}, nil
}

func handleWriteTextFile(sessionID string, cfg PreparedAgentConfig, raw json.RawMessage) (any, *rpcError) {
	var req struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: "invalid fs/write_text_file request"}
	}
	if strings.TrimSpace(req.SessionID) != strings.TrimSpace(sessionID) {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: "sessionId mismatch"}
	}
	path, err := resolveAllowedPath(req.Path, cfg.WriteRoots)
	if err != nil {
		return nil, &rpcError{Code: rpcCodeInvalidParams, Message: err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, &rpcError{Code: rpcCodeInternalError, Message: err.Error()}
	}
	if err := os.WriteFile(path, []byte(req.Content), 0o644); err != nil {
		return nil, &rpcError{Code: rpcCodeInternalError, Message: err.Error()}
	}
	return nil, nil
}

func resolveAllowedPath(rawPath string, roots []string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolvedPath, err := resolveRealPath(absPath)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if isWithinRoot(filepath.Clean(absRoot), resolvedPath) {
			return resolvedPath, nil
		}
	}
	return "", fmt.Errorf("path %q is outside allowed roots", resolvedPath)
}

func isWithinRoot(root string, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if root == target {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func extractText(value any) string {
	var parts []string
	collectText(&parts, value)
	return strings.Join(parts, "\n")
}

func collectText(parts *[]string, value any) {
	switch v := value.(type) {
	case nil:
		return
	case string:
		text := v
		if text != "" {
			*parts = append(*parts, text)
		}
	case []any:
		for _, item := range v {
			collectText(parts, item)
		}
	case map[string]any:
		if typ, _ := v["type"].(string); strings.EqualFold(strings.TrimSpace(typ), "text") {
			if text, _ := v["text"].(string); text != "" {
				*parts = append(*parts, text)
			}
		}
		if content, ok := v["content"]; ok {
			collectText(parts, content)
		}
		for _, key := range []string{"rawOutput", "rawInput"} {
			if item, ok := v[key]; ok {
				collectText(parts, item)
			}
		}
	}
}

func mustMarshalRaw(value any) json.RawMessage {
	if value == nil {
		return json.RawMessage("null")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

func timeNow() int64 {
	return timeNowFunc().UnixMilli()
}

func sinceMillis(startedAt int64) int64 {
	if startedAt <= 0 {
		return 0
	}
	return timeNowFunc().UnixMilli() - startedAt
}

var timeNowFunc = func() time.Time {
	return time.Now()
}

func readTextFileContent(path string, line int, limit int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	if line < 1 {
		line = 1
	}
	reader := bufio.NewReader(file)
	currentLine := 1
	remaining := limit
	var out strings.Builder

	for {
		chunk, readErr := reader.ReadSlice('\n')
		if len(chunk) > 0 {
			if currentLine >= line && (limit <= 0 || remaining > 0) {
				if out.Len()+len(chunk) > maxReadTextFileBytes {
					return "", fmt.Errorf("fs/read_text_file exceeds %d bytes", maxReadTextFileBytes)
				}
				_, _ = out.Write(chunk)
				if limit > 0 && chunk[len(chunk)-1] == '\n' {
					remaining--
					if remaining == 0 {
						return out.String(), nil
					}
				}
			}
			if chunk[len(chunk)-1] == '\n' {
				currentLine++
			}
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if readErr == io.EOF {
			return out.String(), nil
		}
		if readErr != nil {
			return "", readErr
		}
	}
}

func resolveRealPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	base, tail, err := resolveExistingAncestor(absPath)
	if err != nil {
		return "", err
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	resolved = resolvedBase
	for _, part := range tail {
		resolved = filepath.Join(resolved, part)
	}
	return filepath.Clean(resolved), nil
}

func resolveExistingAncestor(path string) (string, []string, error) {
	current := filepath.Clean(path)
	suffix := make([]string, 0, 4)
	for {
		if _, statErr := os.Lstat(current); statErr == nil {
			for left, right := 0, len(suffix)-1; left < right; left, right = left+1, right-1 {
				suffix[left], suffix[right] = suffix[right], suffix[left]
			}
			return current, suffix, nil
		} else if !os.IsNotExist(statErr) {
			return "", nil, statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil, os.ErrNotExist
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func cloneMeta(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type lockedTailWriter struct {
	mu  *sync.Mutex
	buf *cappedTailBuffer
}

func (w *lockedTailWriter) Write(p []byte) (int, error) {
	if w == nil || w.mu == nil || w.buf == nil {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

type cappedTailBuffer struct {
	limit     int
	data      []byte
	truncated bool
}

func (b *cappedTailBuffer) Write(p []byte) (int, error) {
	if b == nil || len(p) == 0 {
		return len(p), nil
	}
	if b.limit <= 0 {
		return len(p), nil
	}
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.truncated = true
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return len(p), nil
}

func (b *cappedTailBuffer) String() string {
	if b == nil || len(b.data) == 0 {
		return ""
	}
	text := string(bytes.ToValidUTF8(b.data, []byte("\n[non-utf8 stderr]\n")))
	if !b.truncated {
		return text
	}
	return "[stderr truncated]\n" + text
}
