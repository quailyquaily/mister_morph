package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
)

type stubBashSubtaskRunner struct {
	req    agent.SubtaskRequest
	result *agent.SubtaskResult
}

func (s *stubBashSubtaskRunner) RunSubtask(_ context.Context, req agent.SubtaskRequest) (*agent.SubtaskResult, error) {
	s.req = req
	return s.result, nil
}

type recordingEventSink struct {
	mu     sync.Mutex
	events []agent.Event
}

func (s *recordingEventSink) HandleEvent(_ context.Context, event agent.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *recordingEventSink) snapshot() []agent.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]agent.Event, len(s.events))
	copy(out, s.events)
	return out
}

func TestContainsTokenBoundary(t *testing.T) {
	cases := []struct {
		name   string
		cmd    string
		needle string
		want   bool
	}{
		{name: "plain", cmd: "cat config.yaml", needle: "config.yaml", want: true},
		{name: "quoted", cmd: "cat \"config.yaml\"", needle: "config.yaml", want: true},
		{name: "subpath", cmd: "cat ./config.yaml", needle: "config.yaml", want: true},
		{name: "parent", cmd: "cat ../config.yaml", needle: "config.yaml", want: true},
		{name: "redir", cmd: "grep x <config.yaml", needle: "config.yaml", want: true},
		{name: "assignment", cmd: "X=config.yaml; echo $X", needle: "config.yaml", want: true},
		{name: "nonmatch_prefix", cmd: "cat myconfig.yaml", needle: "config.yaml", want: false},
		{name: "nonmatch_suffix", cmd: "cat config.yaml.bak", needle: "config.yaml", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := containsTokenBoundary(tc.cmd, tc.needle)
			if got != tc.want {
				t.Fatalf("containsTokenBoundary(%q,%q)=%v, want %v", tc.cmd, tc.needle, got, tc.want)
			}
		})
	}
}

func TestBashCommandDenied(t *testing.T) {
	offending, ok := bashCommandDenied("cat ./config.yaml", []string{"config.yaml"})
	if !ok {
		t.Fatal("expected denied=true")
	}
	if offending != "config.yaml" {
		t.Fatalf("expected offending=config.yaml, got %q", offending)
	}

	if _, ok := bashCommandDenied("echo hello", []string{"config.yaml"}); ok {
		t.Fatal("expected allowed command")
	}
}

func TestBashCommandDeniedTokens_Curl(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{name: "plain", cmd: "curl https://example.com", want: true},
		{name: "upper", cmd: "CURL https://example.com", want: true},
		{name: "subpath", cmd: "/usr/bin/curl https://example.com", want: true},
		{name: "quoted", cmd: "\"curl\" https://example.com", want: true},
		{name: "nonmatch_prefix", cmd: "mycurl https://example.com", want: false},
		{name: "nonmatch_suffix", cmd: "curling https://example.com", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := bashCommandDeniedTokens(tc.cmd, []string{"curl"})
			if ok != tc.want {
				t.Fatalf("bashCommandDeniedTokens(%q)=%v, want %v", tc.cmd, ok, tc.want)
			}
		})
	}
}

func TestReplaceAliasTokenInCommand(t *testing.T) {
	cache := t.TempDir()
	got, err := replaceAliasTokenInCommand("ls file_cache_dir/tmp", "file_cache_dir", cache)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "ls " + filepath.Clean(cache) + "/tmp"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBashTool_Execute_PathAliasInCWD(t *testing.T) {
	cache := t.TempDir()
	state := t.TempDir()
	sub := filepath.Join(state, "scripts")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}

	tool := NewBashTool(true, 5*time.Second, 4096, cache, state)
	out, err := tool.Execute(context.Background(), map[string]any{
		"cmd": "pwd",
		"cwd": "file_state_dir/scripts",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%q)", err, out)
	}
	if !strings.Contains(out, filepath.Clean(sub)) {
		t.Fatalf("expected pwd output to contain %q, got %q", sub, out)
	}
}

func TestBashTool_Execute_PathAliasMissingBaseDir(t *testing.T) {
	cache := t.TempDir()
	tool := NewBashTool(true, 5*time.Second, 4096, cache)
	_, err := tool.Execute(context.Background(), map[string]any{
		"cmd": "cat file_state_dir/note.txt",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "base dir file_state_dir is not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBashTool_Execute_UsesWhitelistedEnvOnly(t *testing.T) {
	t.Setenv("HOME", "/tmp/mm-home")
	t.Setenv("LANG", "C.UTF-8")
	t.Setenv("MISTER_MORPH_API_KEY", "secret_value_should_not_leak")

	tool := NewBashTool(true, 5*time.Second, 4096)
	out, err := tool.Execute(context.Background(), map[string]any{
		"cmd": "env | sort",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "HOME=/tmp/mm-home") {
		t.Fatalf("expected HOME to be preserved, got %q", out)
	}
	if !strings.Contains(out, "LANG=C.UTF-8") {
		t.Fatalf("expected LANG to be preserved, got %q", out)
	}
	if strings.Contains(out, "MISTER_MORPH_API_KEY") || strings.Contains(out, "secret_value_should_not_leak") {
		t.Fatalf("bash env leaked mistermorph secret env: %q", out)
	}
}

func TestBashTool_Execute_AllowsConfiguredExtraEnvVars(t *testing.T) {
	t.Setenv("CUSTOM_API_BASE", "https://example.com")
	t.Setenv("CUSTOM_HTTP_TIMEOUT", "15s")

	tool := NewBashTool(true, 5*time.Second, 4096)
	tool.InjectedEnvVars = []string{"CUSTOM_API_BASE"}

	out, err := tool.Execute(context.Background(), map[string]any{
		"cmd": "env | sort",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "CUSTOM_API_BASE=https://example.com") {
		t.Fatalf("expected allowed env var to be present, got %q", out)
	}
	if strings.Contains(out, "CUSTOM_HTTP_TIMEOUT=15s") {
		t.Fatalf("unexpected non-allowed env var leaked: %q", out)
	}
}

func TestNormalizeInjectedEnvVarName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "FOO_BAR", want: "FOO_BAR"},
		{in: " FOO_BAR ", want: "FOO_BAR"},
		{in: "FOO1", want: "FOO1"},
		{in: "1FOO", want: ""},
		{in: "FOO-BAR", want: ""},
		{in: "FOO BAR", want: ""},
	}
	for _, tc := range cases {
		if got := normalizeInjectedEnvVarName(tc.in); got != tc.want {
			t.Fatalf("normalizeInjectedEnvVarName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBashTool_Execute_EmitsStreamEvents(t *testing.T) {
	tool := NewBashTool(true, 5*time.Second, 4096)
	sink := &recordingEventSink{}
	ctx := agent.WithEventSinkContext(context.Background(), sink)

	out, err := tool.Execute(ctx, map[string]any{
		"cmd": "printf 'alpha\\n'; printf 'beta\\n' 1>&2",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}

	events := sink.snapshot()
	if len(events) == 0 {
		t.Fatal("expected stream events, got none")
	}

	var stdoutSeen bool
	var stderrSeen bool
	for _, event := range events {
		if event.Kind != agent.EventKindToolOutput || event.ToolName != "bash" {
			continue
		}
		if event.Stream == "stdout" && strings.Contains(event.Text, "alpha") {
			stdoutSeen = true
		}
		if event.Stream == "stderr" && strings.Contains(event.Text, "beta") {
			stderrSeen = true
		}
	}
	if !stdoutSeen {
		t.Fatalf("stdout stream event missing, events=%#v", events)
	}
	if !stderrSeen {
		t.Fatalf("stderr stream event missing, events=%#v", events)
	}
}

func TestBashTool_Execute_RunInSubtask(t *testing.T) {
	tool := NewBashTool(true, 5*time.Second, 4096)
	runner := &stubBashSubtaskRunner{
		result: &agent.SubtaskResult{
			TaskID:       "sub_bash",
			Status:       agent.SubtaskStatusDone,
			Summary:      "bash delegated",
			OutputKind:   agent.SubtaskOutputKindJSON,
			OutputSchema: "subtask.bash.result.v1",
			Output: map[string]any{
				"exit_code": float64(0),
			},
		},
	}

	ctx := agent.WithSubtaskRunnerContext(context.Background(), runner)
	out, err := tool.Execute(ctx, map[string]any{
		"cmd":             "printf hello",
		"run_in_subtask":  true,
		"timeout_seconds": 12,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runner.req.Registry != nil {
		t.Fatalf("runner registry should be nil for direct subtask path, got %q", runner.req.Registry.ToolNames())
	}
	if runner.req.OutputSchema != "subtask.bash.result.v1" {
		t.Fatalf("runner output schema = %q", runner.req.OutputSchema)
	}
	if runner.req.RunFunc == nil {
		t.Fatal("runner should receive direct subtask callback")
	}

	var result agent.SubtaskResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal output error = %v, raw=%q", err, out)
	}
	if result.TaskID != "sub_bash" {
		t.Fatalf("result task_id = %q, want sub_bash", result.TaskID)
	}
}

func TestBashTool_Execute_RunInSubtaskDoesNotRecurse(t *testing.T) {
	tool := NewBashTool(true, 5*time.Second, 4096)
	runner := &stubBashSubtaskRunner{
		result: &agent.SubtaskResult{TaskID: "should_not_be_used"},
	}

	ctx := agent.WithSubtaskRunnerContext(context.Background(), runner)
	ctx = agent.WithSubtaskDepth(ctx, 1)
	out, err := tool.Execute(ctx, map[string]any{
		"cmd":            "printf nested",
		"run_in_subtask": true,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}
	if runner.req.Task != "" {
		t.Fatalf("runner should not be called inside subtask, got task=%q", runner.req.Task)
	}
	if !strings.Contains(out, "exit_code: 0") {
		t.Fatalf("expected direct bash output, got %q", out)
	}
}

func TestBashTool_Execute_RunInSubtaskWithoutRunnerFallsBackToDirectSubtask(t *testing.T) {
	tool := NewBashTool(true, 5*time.Second, 4096)
	out, err := tool.Execute(context.Background(), map[string]any{
		"cmd":            "printf fallback",
		"run_in_subtask": true,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v (out=%q)", err, out)
	}

	var result agent.SubtaskResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal output error = %v, raw=%q", err, out)
	}
	if result.Status != agent.SubtaskStatusDone {
		t.Fatalf("status = %q, want done", result.Status)
	}
	if result.OutputSchema != "subtask.bash.result.v1" {
		t.Fatalf("output_schema = %q, want subtask.bash.result.v1", result.OutputSchema)
	}
}

func TestBashTool_Execute_RunInSubtaskFailureReturnsErrorAndEnvelope(t *testing.T) {
	tool := NewBashTool(true, 5*time.Second, 4096)
	runner := &stubBashSubtaskRunner{
		result: &agent.SubtaskResult{
			TaskID:       "sub_fail",
			Status:       agent.SubtaskStatusFailed,
			Summary:      "bash exited with code 7",
			OutputKind:   agent.SubtaskOutputKindJSON,
			OutputSchema: "subtask.bash.result.v1",
			Error:        "bash exited with code 7",
			Output: map[string]any{
				"exit_code": 7,
			},
		},
	}

	ctx := agent.WithSubtaskRunnerContext(context.Background(), runner)
	out, err := tool.Execute(ctx, map[string]any{
		"cmd":            "exit 7",
		"run_in_subtask": true,
	})
	if err == nil {
		t.Fatalf("expected error, got nil (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "code 7") {
		t.Fatalf("error = %v, want code 7", err)
	}

	var result agent.SubtaskResult
	if unmarshalErr := json.Unmarshal([]byte(out), &result); unmarshalErr != nil {
		t.Fatalf("unmarshal output error = %v, raw=%q", unmarshalErr, out)
	}
	if result.Status != agent.SubtaskStatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
}
