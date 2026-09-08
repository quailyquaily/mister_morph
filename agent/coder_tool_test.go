package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/tools"
)

func TestCoderTool_ExecuteCodexUsesSubtaskRunner(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runner := &execDirectSubtaskRunner{}
	var got coderCLIRequest

	tool := newCoderTool(coderToolDeps{
		Runner: runner,
		RunCLI: func(_ context.Context, req coderCLIRequest, emit func(string)) (string, error) {
			got = req
			emit("working")
			return "done output", nil
		},
	})

	var events []Event
	ctx := WithEventSinkContext(context.Background(), EventSinkFunc(func(_ context.Context, event Event) {
		events = append(events, event)
	}))
	raw, err := tool.Execute(ctx, map[string]any{
		"coder": "codex",
		"task":  "inspect the repo",
		"cwd":   dir,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.Backend != coderBackendCodex {
		t.Fatalf("got.Backend = %q, want %q", got.Backend, coderBackendCodex)
	}
	if got.Task != "inspect the repo" {
		t.Fatalf("got.Task = %q, want inspect the repo", got.Task)
	}
	if got.CWD != dir {
		t.Fatalf("got.CWD = %q, want %q", got.CWD, dir)
	}
	if runner.req.RunFunc == nil {
		t.Fatal("coder should run through SubtaskRunner direct path")
	}

	var result SubtaskResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if result.Status != SubtaskStatusDone {
		t.Fatalf("result.Status = %q, want %q", result.Status, SubtaskStatusDone)
	}
	if result.OutputKind != SubtaskOutputKindText {
		t.Fatalf("result.OutputKind = %q, want %q", result.OutputKind, SubtaskOutputKindText)
	}
	if result.Output != "done output" {
		t.Fatalf("result.Output = %#v, want done output", result.Output)
	}

	if len(events) != 1 {
		t.Fatalf("events = %#v, want one tool output event", events)
	}
	if events[0].Kind != EventKindToolOutput || events[0].ToolName != coderToolName || events[0].Stream != "codex" || events[0].Text != "working" {
		t.Fatalf("event = %#v, want coder codex output", events[0])
	}
}

func TestCoderTool_ReturnsFailedEnvelopeOnCLIError(t *testing.T) {
	t.Parallel()

	runner := &execDirectSubtaskRunner{}
	tool := newCoderTool(coderToolDeps{
		Runner: runner,
		RunCLI: func(context.Context, coderCLIRequest, func(string)) (string, error) {
			return "partial output", errors.New("cli failed")
		},
	})

	raw, err := tool.Execute(context.Background(), map[string]any{
		"coder": "claude",
		"task":  "try it",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var result SubtaskResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if result.Status != SubtaskStatusFailed {
		t.Fatalf("result.Status = %q, want %q", result.Status, SubtaskStatusFailed)
	}
	if result.Output != "partial output" {
		t.Fatalf("result.Output = %#v, want partial output", result.Output)
	}
	if !strings.Contains(result.Error, "cli failed") {
		t.Fatalf("result.Error = %q, want cli failed", result.Error)
	}
}

func TestCoderTool_ResolvesCWDPathAliases(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	stateWorkDir := filepath.Join(stateDir, "tasks", "task-a")
	workspaceWorkDir := filepath.Join(workspaceDir, "subdir")
	for _, dir := range []string{stateWorkDir, workspaceWorkDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	for _, tc := range []struct {
		name string
		cwd  string
		want string
		ctx  context.Context
	}{
		{
			name: "file state alias",
			cwd:  "file_state_dir/tasks/task-a",
			want: stateWorkDir,
			ctx:  context.Background(),
		},
		{
			name: "workspace alias",
			cwd:  "workspace_dir/subdir",
			want: workspaceWorkDir,
			ctx:  context.Background(),
		},
		{
			name: "empty defaults to workspace from context",
			cwd:  "",
			want: workspaceWorkDir,
			ctx:  pathroots.WithWorkspaceDir(context.Background(), workspaceWorkDir),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &execDirectSubtaskRunner{}
			var got coderCLIRequest
			tool := newCoderTool(coderToolDeps{
				Roots:  pathroots.New(workspaceDir, cacheDir, stateDir),
				Runner: runner,
				RunCLI: func(_ context.Context, req coderCLIRequest, _ func(string)) (string, error) {
					got = req
					return "ok", nil
				},
			})

			if _, err := tool.Execute(tc.ctx, map[string]any{
				"coder": "codex",
				"task":  "inspect",
				"cwd":   tc.cwd,
			}); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got.CWD != tc.want {
				t.Fatalf("got.CWD = %q, want %q", got.CWD, tc.want)
			}
		})
	}
}

func TestCoderTool_RespectsSubtaskDepthLimit(t *testing.T) {
	t.Parallel()

	runner := &localSubtaskRunner{
		engine: New(noopSubtaskClient{}, tools.NewRegistry(), Config{}, DefaultPromptSpec()),
	}
	called := false
	tool := newCoderTool(coderToolDeps{
		Runner: runner,
		RunCLI: func(context.Context, coderCLIRequest, func(string)) (string, error) {
			called = true
			return "", nil
		},
	})

	raw, err := tool.Execute(WithSubtaskDepth(context.Background(), 1), map[string]any{
		"coder": "codex",
		"task":  "try it",
		"cwd":   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if called {
		t.Fatal("coder CLI should not run after subtask depth limit is reached")
	}
	var result SubtaskResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if result.Status != SubtaskStatusFailed {
		t.Fatalf("result.Status = %q, want %q", result.Status, SubtaskStatusFailed)
	}
	if !strings.Contains(result.Error, "depth limit") {
		t.Fatalf("result.Error = %q, want depth limit", result.Error)
	}
}

func TestCoderToolPromptGuidanceMentionsExplicitCLIRequests(t *testing.T) {
	t.Parallel()

	tool := newCoderTool(coderToolDeps{})
	desc := strings.ToLower(tool.Description())
	for _, want := range []string{"codex", "claude code", "cc"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("Description() = %q, want mention %q", tool.Description(), want)
		}
	}

	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(tool.ParameterSchema()), &schema); err != nil {
		t.Fatalf("json.Unmarshal(ParameterSchema()) error = %v", err)
	}
	coderDesc := strings.ToLower(schema.Properties["coder"].Description)
	for _, want := range []string{"codex", "claude code", "cc"} {
		if !strings.Contains(coderDesc, want) {
			t.Fatalf("coder description = %q, want mention %q", schema.Properties["coder"].Description, want)
		}
	}
	taskDesc := strings.ToLower(schema.Properties["task"].Description)
	for _, want := range []string{"self-contained", "filename"} {
		if !strings.Contains(taskDesc, want) {
			t.Fatalf("task description = %q, want mention %q", schema.Properties["task"].Description, want)
		}
	}
}

func TestCoderTool_ValidateParams(t *testing.T) {
	t.Parallel()

	tool := newCoderTool(coderToolDeps{Runner: &execDirectSubtaskRunner{}})
	for _, tc := range []struct {
		name   string
		params map[string]any
		want   string
	}{
		{name: "missing coder", params: map[string]any{"task": "x"}, want: "coder"},
		{name: "missing task", params: map[string]any{"coder": "codex"}, want: "task"},
		{name: "bad coder", params: map[string]any{"coder": "other", "task": "x"}, want: "unsupported coder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tc.params)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestCoderBuildCLICommand(t *testing.T) {
	t.Parallel()

	codex := buildCoderCLICommand(coderCLIRequest{
		Backend: coderBackendCodex,
		Task:    "x",
		CWD:     "/repo",
	})
	if codex.Command != "codex" {
		t.Fatalf("codex.Command = %q, want codex", codex.Command)
	}
	if want := []string{"exec", "--dangerously-bypass-approvals-and-sandbox", "--json", "-C", "/repo", "-"}; !reflect.DeepEqual(codex.Args, want) {
		t.Fatalf("codex.Args = %#v, want %#v", codex.Args, want)
	}
	if codex.Stdin != "x" {
		t.Fatalf("codex.Stdin = %q, want x", codex.Stdin)
	}

	claude := buildCoderCLICommand(coderCLIRequest{
		Backend: coderBackendClaude,
		Task:    "review",
		CWD:     "/repo",
	})
	if claude.Command != "claude" {
		t.Fatalf("claude.Command = %q, want claude", claude.Command)
	}
	if want := []string{"-p", "review", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--no-session-persistence", "--dangerously-skip-permissions"}; !reflect.DeepEqual(claude.Args, want) {
		t.Fatalf("claude.Args = %#v, want %#v", claude.Args, want)
	}
	if claude.Stdin != "" {
		t.Fatalf("claude.Stdin = %q, want empty", claude.Stdin)
	}
}

func TestRunCoderCLIIncludesStderrTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executable uses /bin/sh")
	}

	binDir := t.TempDir()
	writeFakeCoderExecutable(t, binDir, "codex", "printf 'stderr tail' >&2\nexit 7\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	output, err := runCoderCLI(context.Background(), coderCLIRequest{
		Backend: coderBackendCodex,
		Task:    "x",
		CWD:     t.TempDir(),
	}, nil)
	if output != "" {
		t.Fatalf("output = %q, want empty", output)
	}
	if err == nil || !strings.Contains(err.Error(), "stderr tail") {
		t.Fatalf("runCoderCLI() error = %v, want stderr tail", err)
	}
	if !strings.Contains(err.Error(), "codex exited with code 7") {
		t.Fatalf("runCoderCLI() error = %v, want exit code 7", err)
	}
	if strings.Count(err.Error(), "stderr tail") != 1 {
		t.Fatalf("runCoderCLI() error = %v, want stderr tail exactly once", err)
	}
}

func TestRunCoderCLIUsesPathExtraForCommandLookupAndChildEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executable uses /bin/sh")
	}

	binDir := t.TempDir()
	writeFakeCoderExecutable(t, binDir, "codex", "case \"$PATH\" in \""+binDir+"\":*) printf 'path-extra-ok\\n' ;; *) printf 'missing path_extra: %s' \"$PATH\" >&2; exit 8 ;; esac\n")
	t.Setenv("PATH", "/path-that-does-not-exist")

	output, err := runCoderCLI(context.Background(), coderCLIRequest{
		Backend:   coderBackendCodex,
		Task:      "x",
		CWD:       t.TempDir(),
		PathExtra: []string{" " + binDir + " "},
	}, nil)
	if err != nil {
		t.Fatalf("runCoderCLI() error = %v", err)
	}
	if output != "path-extra-ok" {
		t.Fatalf("output = %q, want path-extra-ok", output)
	}
}

func TestRunCoderCLIContextCancelStopsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell executable uses /bin/sh")
	}

	binDir := t.TempDir()
	writeFakeCoderExecutable(t, binDir, "codex", "exec sleep 10\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runCoderCLI(ctx, coderCLIRequest{
		Backend: coderBackendCodex,
		Task:    "x",
		CWD:     t.TempDir(),
	}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runCoderCLI() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("runCoderCLI() took %v after context cancellation", elapsed)
	}
}

func writeFakeCoderExecutable(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
}

func TestCoderStreamCollectorClaude(t *testing.T) {
	t.Parallel()

	var chunks []string
	collector := newCoderStreamCollector(coderBackendClaude, func(text string) {
		chunks = append(chunks, text)
	})
	for _, line := range []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hel"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"result","result":"hello final","is_error":false}`,
	} {
		if err := collector.ConsumeLine([]byte(line)); err != nil {
			t.Fatalf("ConsumeLine(%s) error = %v", line, err)
		}
	}
	if got := strings.Join(chunks, ""); got != "hello" {
		t.Fatalf("chunks = %#v joined %q, want hello", chunks, got)
	}
	if got := collector.Output(); got != "hello final" {
		t.Fatalf("collector.Output() = %q, want hello final", got)
	}
}

func TestCoderStreamCollectorClaudeToolProgress(t *testing.T) {
	var chunks []string
	collector := newCoderStreamCollector(coderBackendClaude, func(text string) { chunks = append(chunks, text) })
	for _, line := range []string{
		`{"type":"system","subtype":"init","session_id":"session-1"}`,
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","id":"tool-1","name":"Read","input":{}}}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool-1","name":"Read","input":{"file_path":"source.go"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"source content"}]}}`,
		`{"type":"result","result":"finished","is_error":false}`,
	} {
		if err := collector.ConsumeLine([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	progress := strings.Join(chunks, "")
	for _, want := range []string{"init", "Read", "source.go", "tool-1", "completed"} {
		if !strings.Contains(progress, want) {
			t.Fatalf("progress %q missing %q", progress, want)
		}
	}
	if collector.Output() != "finished" {
		t.Fatalf("output = %q", collector.Output())
	}
}

func TestRunClaudeTimeoutPreservesDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses /bin/sh")
	}
	binDir := t.TempDir()
	logRoot := t.TempDir()
	writeFakeCoderExecutable(t, binDir, "claude", "while [ $# -gt 0 ]; do\n  if [ \"$1\" = '--debug-file' ]; then shift; printf 'debug enabled\\n' > \"$1\"; fi\n  shift\ndone\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\"}'\nprintf 'waiting for upstream\\n' >&2\nexec sleep 10\n")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var chunks []string
	_, err := runCoderCLI(ctx, coderCLIRequest{Backend: coderBackendClaude, Task: "x", CWD: t.TempDir(), LogRoot: logRoot, PathExtra: []string{binDir}}, func(text string) { chunks = append(chunks, text) })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	for _, want := range []string{"waiting for upstream", "init", "diagnostics:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	progress := strings.Join(chunks, "")
	if !strings.Contains(progress, "waiting for upstream") {
		t.Fatalf("stderr not streamed: %q", progress)
	}
	dirs, err := filepath.Glob(filepath.Join(logRoot, "coder", "claude-*"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("diagnostic directories = %v, err=%v", dirs, err)
	}
	for name, want := range map[string]string{"stdout.jsonl": `"subtype":"init"`, "stderr.log": "waiting for upstream", "debug.log": "debug enabled"} {
		data, err := os.ReadFile(filepath.Join(dirs[0], name))
		if err != nil || !strings.Contains(string(data), want) {
			t.Fatalf("%s = %q, err=%v", name, data, err)
		}
	}
}

func TestCoderStreamCollectorCodex(t *testing.T) {
	t.Parallel()

	var chunks []string
	collector := newCoderStreamCollector(coderBackendCodex, func(text string) {
		chunks = append(chunks, text)
	})
	for _, line := range []string{
		`{"method":"item/agentMessage/delta","params":{"delta":"Hi "}}`,
		`{"type":"item.completed","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hi final"}]}}`,
	} {
		if err := collector.ConsumeLine([]byte(line)); err != nil {
			t.Fatalf("ConsumeLine(%s) error = %v", line, err)
		}
	}
	if got := strings.Join(chunks, ""); got != "Hi final" {
		t.Fatalf("chunks = %#v joined %q, want Hi final", chunks, got)
	}
	if got := collector.Output(); got != "Hi final" {
		t.Fatalf("collector.Output() = %q, want Hi final", got)
	}
}

func TestCoderStreamCollectorCodexCurrentJSONL(t *testing.T) {
	t.Parallel()

	var chunks []string
	collector := newCoderStreamCollector(coderBackendCodex, func(text string) {
		chunks = append(chunks, text)
	})
	for _, line := range []string{
		`{"type":"thread.started","thread_id":"thread_1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/bin/bash -lc 'printf hi'","aggregated_output":"","exit_code":null,"status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/bin/bash -lc 'printf hi'","aggregated_output":"hi","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"done"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
	} {
		if err := collector.ConsumeLine([]byte(line)); err != nil {
			t.Fatalf("ConsumeLine(%s) error = %v", line, err)
		}
	}
	for _, want := range []string{"thread.started", "turn.started", "command_execution", "in_progress", "completed", "printf hi", "hi", "done"} {
		if got := strings.Join(chunks, ""); !strings.Contains(got, want) {
			t.Fatalf("progress %q missing %q", got, want)
		}
	}
	if got := collector.Output(); got != "done" {
		t.Fatalf("collector.Output() = %q, want done", got)
	}
}

func TestRunCodexTimeoutPreservesDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses /bin/sh")
	}
	binDir, logRoot := t.TempDir(), t.TempDir()
	writeFakeCoderExecutable(t, binDir, "codex", "printf '%s\\n' '{\"type\":\"turn.started\"}'\nprintf 'waiting for upstream\\n' >&2\nexec sleep 10\n")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var progress []string
	_, err := runCoderCLI(ctx, coderCLIRequest{Backend: coderBackendCodex, Task: "x", CWD: t.TempDir(), LogRoot: logRoot, PathExtra: []string{binDir}}, func(text string) { progress = append(progress, text) })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	for _, want := range []string{"waiting for upstream", "turn.started", "diagnostics:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	for _, want := range []string{"[codex] started pid=", "remaining task time:", "waiting for upstream", "diagnostics:"} {
		if got := strings.Join(progress, ""); !strings.Contains(got, want) {
			t.Fatalf("progress %q missing %q", got, want)
		}
	}
	dirs, err := filepath.Glob(filepath.Join(logRoot, "coder", "codex-*"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("diagnostics = %v, err=%v", dirs, err)
	}
	for name, want := range map[string]string{"stdout.jsonl": `"turn.started"`, "stderr.log": "waiting for upstream"} {
		data, err := os.ReadFile(filepath.Join(dirs[0], name))
		if err != nil || !strings.Contains(string(data), want) {
			t.Fatalf("%s = %q, err=%v", name, data, err)
		}
	}
}

func TestCoderStreamCollectorCodexStatusAndFailure(t *testing.T) {
	var summaries []string
	collector := newCoderStreamCollector(coderBackendCodex, nil)
	collector.progress = func(summary, _ string) { summaries = append(summaries, summary) }
	for _, line := range []string{
		`{"type":"item.started","item":{"id":"cmd","type":"command_execution","status":"in_progress","command":"echo private-command"}}`,
		`{"type":"item.completed","item":{"id":"cmd","type":"command_execution","status":"completed","aggregated_output":"private-output","exit_code":0}}`,
		`{"type":"item.completed","item":{"id":"patch","type":"file_change","status":"completed","changes":[{"path":"private-path","kind":"update"}]}}`,
		`{"type":"item.started","item":{"id":"mcp","type":"mcp_tool_call","status":"in_progress"}}`,
		`{"type":"turn.failed","error":{"message":"upstream disconnected"}}`,
	} {
		if err := collector.ConsumeLine([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	got := strings.Join(summaries, "")
	for _, want := range []string{"command_execution", "completed", "file_change", "mcp_tool_call", "turn.failed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summaries %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "private") {
		t.Fatalf("summary leaked raw details: %q", got)
	}
	if collector.Error() != "upstream disconnected" {
		t.Fatalf("error = %q", collector.Error())
	}
	if collector.Output() != "" {
		t.Fatalf("status polluted final output: %q", collector.Output())
	}
}

func TestCoderStreamCollectorCodexRecoversFromStreamError(t *testing.T) {
	collector := newCoderStreamCollector(coderBackendCodex, nil)
	for _, line := range []string{
		`{"type":"error","message":"Reconnecting..."}`,
		`{"type":"item.completed","item":{"id":"answer","type":"agent_message","text":"done"}}`,
		`{"type":"turn.completed","usage":{}}`,
	} {
		if err := collector.ConsumeLine([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if collector.Error() != "" || collector.Output() != "done" {
		t.Fatalf("successful turn: output=%q error=%q", collector.Output(), collector.Error())
	}
}
