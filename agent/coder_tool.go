package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

const (
	coderToolName      = "coder"
	coderBackendCodex  = "codex"
	coderBackendClaude = "claude"

	coderStderrTailBytes = 64 * 1024
	coderScannerMaxBytes = 4 * 1024 * 1024
)

type coderTool struct {
	deps coderToolDeps
}

type coderCLIRequest struct {
	Backend string
	Task    string
	CWD     string
}

type coderCLICommand struct {
	Command string
	Args    []string
	Stdin   string
	Dir     string
}

type coderCLIRunFunc func(ctx context.Context, req coderCLIRequest, emit func(string)) (string, error)

func newCoderTool(deps coderToolDeps) *coderTool {
	return &coderTool{deps: deps}
}

func (t *coderTool) Name() string { return coderToolName }

func (t *coderTool) Description() string {
	return "Delegate a repository coding task to the local Codex or Claude Code CLI. " +
		"Use this when the user explicitly asks for Codex, Claude Code, or cc to handle code edits, review fixes, tests, or file generation. " +
		"The child coder runs non-interactively with approvals bypassed and returns a SubtaskResult envelope."
}

func (t *coderTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"coder": map[string]any{
				"type":        "string",
				"enum":        []string{coderBackendCodex, coderBackendClaude},
				"description": "CLI backend to run: use codex for Codex CLI; use claude for Claude Code or cc.",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Self-contained task prompt for the coding CLI. Preserve requested filenames, constraints, and expected output.",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Optional working directory for the coding CLI. Supports workspace_dir, file_cache_dir, and file_state_dir aliases. Omit to use the current workspace.",
			},
		},
		"required": []string{"coder", "task"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *coderTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	req, err := coderRequestFromParams(ctx, params, t.deps.Roots)
	if err != nil {
		return "", err
	}

	runner := t.deps.Runner
	if runner == nil {
		return "", fmt.Errorf("subtask runner unavailable")
	}
	runCLI := t.deps.RunCLI
	if runCLI == nil {
		runCLI = runCoderCLI
	}

	result, err := runner.RunSubtask(ctx, SubtaskRequest{
		RunFunc: func(runCtx context.Context) (*SubtaskResult, error) {
			output, runErr := runCLI(runCtx, req, func(text string) {
				text = strings.TrimRight(text, "\x00")
				if text == "" {
					return
				}
				EmitEvent(runCtx, nil, Event{
					Kind:     EventKindToolOutput,
					ToolName: coderToolName,
					Stream:   req.Backend,
					Text:     text,
					Status:   "running",
				})
			})
			if runErr != nil {
				out := FailedSubtaskResult("", runErr)
				out.OutputKind = SubtaskOutputKindText
				out.Output = output
				return out, nil
			}
			return &SubtaskResult{
				Status:     SubtaskStatusDone,
				Summary:    summarizeSubtaskText(output),
				OutputKind: SubtaskOutputKindText,
				Output:     output,
			}, nil
		},
	})
	if err != nil {
		if result == nil {
			result = FailedSubtaskResult("", err)
		}
	}
	if result == nil {
		result = FailedSubtaskResult("", fmt.Errorf("coder subtask returned nil result"))
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func coderRequestFromParams(ctx context.Context, params map[string]any, roots pathroots.PathRoots) (coderCLIRequest, error) {
	backend, _ := params["coder"].(string)
	backend = strings.ToLower(strings.TrimSpace(backend))
	if backend == "" {
		return coderCLIRequest{}, fmt.Errorf("missing required param: coder")
	}
	if backend != coderBackendCodex && backend != coderBackendClaude {
		return coderCLIRequest{}, fmt.Errorf("unsupported coder: %s", backend)
	}
	task, _ := params["task"].(string)
	task = strings.TrimSpace(task)
	if task == "" {
		return coderCLIRequest{}, fmt.Errorf("missing required param: task")
	}
	cwd, _ := params["cwd"].(string)
	resolvedCWD, err := resolveCoderCWD(ctx, roots, cwd)
	if err != nil {
		return coderCLIRequest{}, err
	}
	return coderCLIRequest{
		Backend: backend,
		Task:    task,
		CWD:     resolvedCWD,
	}, nil
}

func resolveCoderCWD(ctx context.Context, roots pathroots.PathRoots, raw string) (string, error) {
	roots = pathroots.Resolve(ctx, roots)
	cwd := strings.TrimSpace(raw)
	if cwd == "" {
		if workspaceDir := strings.TrimSpace(roots.WorkspaceDir); workspaceDir != "" {
			cwd = workspaceDir
		} else {
			cwd = "."
		}
	}
	cwd = pathutil.ExpandHomePath(cwd)
	alias, rest := detectCoderPathAlias(cwd)
	var abs string
	var err error
	if alias != "" {
		abs, err = resolveCoderAliasedCWD(roots, alias, rest)
	} else if filepath.IsAbs(cwd) {
		abs, err = filepath.Abs(filepath.Clean(cwd))
	} else if workspaceDir := strings.TrimSpace(roots.WorkspaceDir); workspaceDir != "" {
		abs, err = filepath.Abs(filepath.Join(pathutil.ExpandHomePath(workspaceDir), cwd))
	} else {
		abs, err = filepath.Abs(cwd)
	}
	if err != nil {
		return "", fmt.Errorf("resolve coder cwd: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("resolve coder cwd: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("coder cwd %q is not a directory", abs)
	}
	return abs, nil
}

func detectCoderPathAlias(userPath string) (string, string) {
	trimmed := strings.TrimLeft(userPath, "/\\")
	lower := strings.ToLower(trimmed)
	prefixes := []struct {
		alias  string
		prefix string
	}{
		{alias: "workspace_dir", prefix: "workspace_dir/"},
		{alias: "workspace_dir", prefix: "workspace_dir\\"},
		{alias: "file_cache_dir", prefix: "file_cache_dir/"},
		{alias: "file_cache_dir", prefix: "file_cache_dir\\"},
		{alias: "file_state_dir", prefix: "file_state_dir/"},
		{alias: "file_state_dir", prefix: "file_state_dir\\"},
	}
	switch lower {
	case "workspace_dir", "file_cache_dir", "file_state_dir":
		return lower, ""
	}
	for _, item := range prefixes {
		if !strings.HasPrefix(lower, item.prefix) {
			continue
		}
		return item.alias, strings.TrimLeft(trimmed[len(item.prefix):], "/\\")
	}
	return "", userPath
}

func resolveCoderAliasedCWD(roots pathroots.PathRoots, alias string, rest string) (string, error) {
	base := strings.TrimSpace(roots.BaseDir(alias))
	if base == "" {
		return "", fmt.Errorf("base dir %s is not configured", alias)
	}
	baseAbs, err := filepath.Abs(pathutil.ExpandHomePath(base))
	if err != nil {
		return "", err
	}
	rest = strings.TrimLeft(strings.TrimSpace(rest), "/\\")
	if rest == "" {
		return baseAbs, nil
	}
	candidate := filepath.Join(baseAbs, rest)
	candAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if !pathutil.IsWithinDir(baseAbs, candAbs) {
		return "", fmt.Errorf("refusing to access outside allowed base dir %s", alias)
	}
	return candAbs, nil
}

func buildCoderCLICommand(req coderCLIRequest) coderCLICommand {
	switch strings.TrimSpace(req.Backend) {
	case coderBackendCodex:
		return coderCLICommand{
			Command: "codex",
			Args: []string{
				"exec",
				"--dangerously-bypass-approvals-and-sandbox",
				"--json",
				"-C",
				strings.TrimSpace(req.CWD),
				"-",
			},
			Stdin: req.Task,
			Dir:   strings.TrimSpace(req.CWD),
		}
	case coderBackendClaude:
		return coderCLICommand{
			Command: "claude",
			Args: []string{
				"-p",
				req.Task,
				"--output-format",
				"stream-json",
				"--verbose",
				"--include-partial-messages",
				"--no-session-persistence",
				"--dangerously-skip-permissions",
			},
			Dir: strings.TrimSpace(req.CWD),
		}
	default:
		return coderCLICommand{}
	}
}

func runCoderCLI(ctx context.Context, req coderCLIRequest, emit func(string)) (string, error) {
	spec := buildCoderCLICommand(req)
	if strings.TrimSpace(spec.Command) == "" {
		return "", fmt.Errorf("unsupported coder: %s", req.Backend)
	}
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	if spec.Dir != "" {
		cmd.Dir = spec.Dir
	}
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	collector := newCoderStreamCollector(req.Backend, emit)
	var stderrTail coderTailBuffer
	stderrTail.Limit = coderStderrTailBytes
	var wg sync.WaitGroup
	var stdoutErr error
	var stderrErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		stdoutErr = readCoderStdout(stdout, collector)
	}()
	go func() {
		defer wg.Done()
		_, stderrErr = io.Copy(&stderrTail, stderr)
	}()

	waitErr := cmd.Wait()
	wg.Wait()
	output := collector.Output()
	if stdoutErr != nil {
		return output, stdoutErr
	}
	if stderrErr != nil {
		return output, stderrErr
	}
	if waitErr != nil {
		if ctx != nil && ctx.Err() != nil {
			return output, ctx.Err()
		}
		return output, coderExitError(req.Backend, waitErr, stderrTail.String())
	}
	if errText := collector.Error(); errText != "" {
		return output, fmt.Errorf("%s failed: %s", req.Backend, errText)
	}
	return output, nil
}

func readCoderStdout(r io.Reader, collector *coderStreamCollector) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), coderScannerMaxBytes)
	for scanner.Scan() {
		if err := collector.ConsumeLine(scanner.Bytes()); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func coderExitError(backend string, err error, stderrTail string) error {
	code := -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	stderrTail = strings.TrimSpace(stderrTail)
	if stderrTail == "" {
		return fmt.Errorf("%s exited with code %d", backend, code)
	}
	return fmt.Errorf("%s exited with code %d: %s", backend, code, stderrTail)
}

type coderStreamCollector struct {
	backend string
	emit    func(string)

	emitted       strings.Builder
	final         string
	errText       string
	itemOutputLen map[string]int
}

func newCoderStreamCollector(backend string, emit func(string)) *coderStreamCollector {
	return &coderStreamCollector{
		backend:       strings.ToLower(strings.TrimSpace(backend)),
		emit:          emit,
		itemOutputLen: map[string]int{},
	}
}

func (c *coderStreamCollector) ConsumeLine(line []byte) error {
	if c == nil {
		return nil
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(line, &payload); err != nil {
		c.addDelta(string(line) + "\n")
		return nil
	}
	switch c.backend {
	case coderBackendClaude:
		c.consumeClaude(payload)
	default:
		c.consumeCodex(payload)
	}
	return nil
}

func (c *coderStreamCollector) Output() string {
	if c == nil {
		return ""
	}
	if text := strings.TrimSpace(c.final); text != "" {
		return text
	}
	return strings.TrimSpace(c.emitted.String())
}

func (c *coderStreamCollector) Error() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.errText)
}

func (c *coderStreamCollector) addDelta(text string) {
	if c == nil || text == "" {
		return
	}
	c.emitted.WriteString(text)
	if c.emit != nil {
		c.emit(text)
	}
}

func (c *coderStreamCollector) setAssistantText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	emitted := c.emitted.String()
	if strings.HasPrefix(text, emitted) {
		c.addDelta(strings.TrimPrefix(text, emitted))
	} else if emitted == "" {
		c.addDelta(text)
	} else if !strings.Contains(emitted, text) {
		c.addDelta("\n" + text)
	}
	c.final = text
}

func (c *coderStreamCollector) consumeClaude(payload map[string]any) {
	switch strings.TrimSpace(stringField(payload, "type")) {
	case "stream_event":
		if text := claudeStreamDelta(payload["event"]); text != "" {
			c.addDelta(text)
		}
	case "assistant":
		if text := textFromValue(payload["message"]); text != "" {
			c.setAssistantText(text)
		}
	case "result":
		if text := stringField(payload, "result"); text != "" {
			c.final = text
		}
		if boolField(payload, "is_error") {
			c.errText = firstNonEmptyString(stringField(payload, "result"), stringField(payload, "error"))
		}
	}
}

func (c *coderStreamCollector) consumeCodex(payload map[string]any) {
	method := stringField(payload, "method")
	if method == "item/agentMessage/delta" {
		if params, ok := mapField(payload, "params"); ok {
			if text := stringField(params, "delta"); text != "" {
				c.addDelta(text)
				return
			}
		}
	}
	typ := stringField(payload, "type")
	if strings.Contains(strings.ToLower(typ), "delta") {
		if text := firstNonEmptyString(stringField(payload, "delta"), stringField(payload, "text")); text != "" {
			c.addDelta(text)
			return
		}
	}
	if item, ok := mapField(payload, "item"); ok && isAssistantCoderItem(item) {
		if text := firstNonEmptyString(stringField(item, "text"), textFromValue(item["content"])); text != "" {
			c.setAssistantText(text)
			return
		}
	}
	if item, ok := mapField(payload, "item"); ok && strings.TrimSpace(stringField(item, "type")) == "command_execution" {
		c.consumeCodexCommandItem(item)
		return
	}
	if text := firstNonEmptyString(stringField(payload, "output"), stringField(payload, "result"), stringField(payload, "final")); text != "" {
		c.final = text
	}
	if errText := firstNonEmptyString(stringField(payload, "error"), stringField(payload, "message")); errText != "" && strings.Contains(strings.ToLower(typ), "error") {
		c.errText = errText
	}
}

func claudeStreamDelta(value any) string {
	event, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if delta, ok := mapField(event, "delta"); ok {
		if text := stringField(delta, "text"); text != "" {
			return text
		}
	}
	return ""
}

func isAssistantCoderItem(item map[string]any) bool {
	role := strings.ToLower(strings.TrimSpace(stringField(item, "role")))
	typ := strings.ToLower(strings.TrimSpace(stringField(item, "type")))
	return role == "assistant" || typ == "message" || typ == "assistant_message" || typ == "agent_message"
}

func (c *coderStreamCollector) consumeCodexCommandItem(item map[string]any) {
	id := strings.TrimSpace(stringField(item, "id"))
	command := strings.TrimSpace(stringField(item, "command"))
	status := strings.TrimSpace(stringField(item, "status"))
	if status == "in_progress" && command != "" {
		c.addDelta("$ " + command + "\n")
	}

	output := stringField(item, "aggregated_output")
	if output == "" {
		return
	}
	emittedLen := c.itemOutputLen[id]
	runes := []rune(output)
	if emittedLen > len(runes) {
		emittedLen = 0
	}
	if emittedLen < len(runes) {
		c.addDelta(string(runes[emittedLen:]))
		c.itemOutputLen[id] = len(runes)
	}
}

func textFromValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, item := range v {
			b.WriteString(textFromValue(item))
		}
		return b.String()
	case map[string]any:
		if text := firstNonEmptyString(stringField(v, "text"), stringField(v, "output_text")); text != "" {
			return text
		}
		if content, ok := v["content"]; ok {
			return textFromValue(content)
		}
		if message, ok := v["message"]; ok {
			return textFromValue(message)
		}
	}
	return ""
}

func mapField(m map[string]any, key string) (map[string]any, bool) {
	value, ok := m[key]
	if !ok {
		return nil, false
	}
	out, ok := value.(map[string]any)
	return out, ok
}

func stringField(m map[string]any, key string) string {
	value, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func boolField(m map[string]any, key string) bool {
	value, ok := m[key]
	if !ok {
		return false
	}
	b, ok := value.(bool)
	return ok && b
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type coderTailBuffer struct {
	Limit int
	buf   []byte
}

func (b *coderTailBuffer) Write(p []byte) (int, error) {
	if b == nil {
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	if b.Limit > 0 && len(b.buf) > b.Limit {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.Limit:]...)
	}
	return len(p), nil
}

func (b *coderTailBuffer) String() string {
	if b == nil {
		return ""
	}
	return string(bytes.ToValidUTF8(b.buf, []byte("\n[non-utf8 output]\n")))
}
