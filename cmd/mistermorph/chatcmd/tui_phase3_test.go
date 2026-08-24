package chatcmd

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/quailyquaily/mistermorph/agent"
)

func newPhase3CallbackSession() (*chatSession, *bytes.Buffer, *[]any) {
	var output bytes.Buffer
	messages := make([]any, 0)
	sess := &chatSession{
		writer:        &output,
		fileSnapshots: make(map[string]string),
		sendMsg: func(message any) {
			messages = append(messages, message)
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	configureChatSessionCallbacks(sess, logger)
	return sess, &output, &messages
}

func TestChatToolCallbacksKeepStartOutOfTranscript(t *testing.T) {
	sess, output, messages := newPhase3CallbackSession()
	call := agent.ToolCall{Name: "bash", Params: map[string]any{"cmd": "go test ./...", "timeout": 30}}

	sess.onToolCallStart(nil, call)
	if output.Len() != 0 {
		t.Fatalf("tool start wrote transcript output %q", output.String())
	}
	if len(*messages) != 1 {
		t.Fatalf("tool start messages = %#v, want one activity update", *messages)
	}
	activity, ok := (*messages)[0].(thinkingMsg)
	if !ok || !activity.on || activity.message != "bash · cmd: go test ./... · timeout: 30" {
		t.Fatalf("tool start activity = %#v", (*messages)[0])
	}

	sess.onToolCallDone(nil, call, "ok", nil)
	plainOutput := ansi.Strip(output.String())
	if got := strings.Count(plainOutput, "✓ bash · cmd: go test ./... · timeout: 30"); got != 1 {
		t.Fatalf("tool completion summary count = %d, output = %q", got, output.String())
	}
	if !strings.Contains(plainOutput, "  └ ok") {
		t.Fatalf("tool completion missing output preview: %q", output.String())
	}
	if strings.ContainsAny(plainOutput, "{}") {
		t.Fatalf("tool completion still uses JSON params: %q", plainOutput)
	}
	if strings.Contains(plainOutput, "used bash") {
		t.Fatalf("transcript still contains tool-start noise: %q", output.String())
	}
}

func TestChatToolFailureKeepsTargetAndError(t *testing.T) {
	sess, output, _ := newPhase3CallbackSession()
	call := agent.ToolCall{Name: "bash", Params: map[string]any{"cmd": "false", "timeout": 30}}

	sess.onToolCallDone(nil, call, "", errors.New("exit status 1"))
	plainOutput := ansi.Strip(output.String())
	for _, want := range []string{"× bash · cmd: false · timeout: 30", "  error: exit status 1"} {
		if !strings.Contains(plainOutput, want) {
			t.Fatalf("tool failure missing %q: %q", want, output.String())
		}
	}
}

func TestChatToolFailureEscapesTerminalControlCharacters(t *testing.T) {
	sess, output, _ := newPhase3CallbackSession()
	call := agent.ToolCall{Name: "web_search", Params: map[string]any{"q": "status"}}

	sess.onToolCallDone(nil, call, "", errors.New("remote error: \x1b[2Jclear\x1b]0;title\x07"))
	got := output.String()
	for _, unsafe := range []string{"\x1b[2J", "\x1b]0;title\x07"} {
		if strings.Contains(got, unsafe) {
			t.Fatalf("tool failure contains executable terminal control %q: %q", unsafe, got)
		}
	}
	for _, visible := range []string{`\x1b[2Jclear`, `\x1b]0;title\x07`} {
		if !strings.Contains(got, visible) {
			t.Fatalf("tool failure missing visible control sequence %q: %q", visible, got)
		}
	}
}

func TestChatToolFailureDoesNotRepeatEngineErrorInOutput(t *testing.T) {
	sess, output, _ := newPhase3CallbackSession()
	call := agent.ToolCall{Name: "bash", Params: map[string]any{"cmd": "false"}}
	observation := strings.Join([]string{
		"exit_code: 1",
		"stdout_truncated: false",
		"stderr_truncated: false",
		"stdout:",
		"",
		"",
		"stderr:",
		"assertion failed",
		"",
		"error: bash exited with code 1",
	}, "\n")

	sess.onToolCallDone(nil, call, observation, errors.New("bash exited with code 1"))
	plainOutput := ansi.Strip(output.String())
	if !strings.Contains(plainOutput, "  └ stderr: assertion failed") {
		t.Fatalf("tool failure missing stderr preview: %q", output.String())
	}
	if got := strings.Count(plainOutput, "bash exited with code 1"); got != 1 {
		t.Fatalf("tool failure error count = %d, want 1: %q", got, output.String())
	}
}

func TestChatActivityNormalizationPreservesParameterSpacing(t *testing.T) {
	t.Parallel()

	input := "bash · cmd: printf 'a  b'"
	if got := normalizeActivityText(input); got != input {
		t.Fatalf("normalizeActivityText() = %q, want %q", got, input)
	}
}

func TestFormatChatToolTranscriptUsesHangingIndent(t *testing.T) {
	t.Parallel()

	call := agent.ToolCall{Name: "bash", Params: map[string]any{
		"cmd": "printf 'a very long tool argument that must wrap cleanly'",
		"env": map[string]any{"CI": true, "MODE": "test"},
	}}
	got := formatChatToolTranscript(call, 28)
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[0], "bash · cmd:") {
		t.Fatalf("tool transcript first line = %q, want compact tool and params", lines[0])
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("tool transcript continuation lacks hanging indent: %q", line)
		}
		if width := ansi.StringWidth(line); width > 28 {
			t.Fatalf("tool transcript line width = %d, want <= 28: %q", width, line)
		}
	}
	if strings.ContainsAny(got, "{}") {
		t.Fatalf("tool transcript still uses JSON params: %q", got)
	}
	if !strings.Contains(got, "\n  env:\n") || !strings.Contains(got, "    MODE: test") {
		t.Fatalf("tool transcript does not keep nested params readable: %q", got)
	}
}

func TestFormatChatToolParamsEscapeTerminalControlsInNames(t *testing.T) {
	t.Parallel()

	call := agent.ToolCall{Name: "bash", Params: map[string]any{
		"cmd":                                 "true",
		"unsafe\nline\x1b[2J\x1b]0;title\x07": "value",
	}}
	for name, got := range map[string]string{
		"activity":   formatChatToolActivity(call),
		"transcript": formatChatToolTranscript(call, 80),
	} {
		for _, unsafe := range []string{"\x1b[2J", "\x1b]0;title\x07"} {
			if strings.Contains(got, unsafe) {
				t.Fatalf("%s contains executable terminal control %q: %q", name, unsafe, got)
			}
		}
		for _, visible := range []string{`unsafe\nline`, `\x1b[2J`, `\x1b]0;title\x07`} {
			if !strings.Contains(got, visible) {
				t.Fatalf("%s missing visible control sequence %q: %q", name, visible, got)
			}
		}
	}
}

func TestFormatChatToolOutputUsesShellContentInsteadOfEnvelope(t *testing.T) {
	t.Parallel()

	observation := strings.Join([]string{
		"exit_code: 0",
		"stdout_truncated: false",
		"stderr_truncated: false",
		"stdout:",
		"package one passed",
		"package two passed",
		"",
		"stderr:",
		"warning: slow test",
	}, "\n")
	got := strings.Join(formatChatToolOutput(agent.ToolCall{Name: "bash"}, observation, 80), "\n")
	for _, want := range []string{"  └ package one passed", "    package two passed", "    stderr: warning: slow test"} {
		if !strings.Contains(got, want) {
			t.Fatalf("shell output preview missing %q: %q", want, got)
		}
	}
	for _, unwanted := range []string{"exit_code:", "stdout_truncated:", "stderr_truncated:", "stdout:"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("shell output preview contains envelope field %q: %q", unwanted, got)
		}
	}
}

func TestFormatChatToolOutputLimitsRenderedLines(t *testing.T) {
	t.Parallel()

	got := formatChatToolOutput(agent.ToolCall{Name: "read_file"}, "one\ntwo\nthree\nfour\nfive\nsix\nseven", 80)
	want := []string{
		"  └ one",
		"    two",
		"    … 3 lines omitted",
		"    six",
		"    seven",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tool output preview = %#v, want %#v", got, want)
	}

	wrapped := formatChatToolOutput(agent.ToolCall{Name: "read_file"}, strings.Repeat("x", 100), 16)
	if len(wrapped) > 5 {
		t.Fatalf("wrapped output lines = %d, want <= 5: %#v", len(wrapped), wrapped)
	}
	for _, line := range wrapped {
		if width := ansi.StringWidth(line); width > 16 {
			t.Fatalf("wrapped output width = %d, want <= 16: %q", width, line)
		}
	}
}

func TestFormatChatToolOutputEscapesTerminalControlCharacters(t *testing.T) {
	t.Parallel()

	got := strings.Join(formatChatToolOutput(
		agent.ToolCall{Name: "read_file"},
		"safe\x1b[2Jclear\x1b]0;title\x07",
		80,
	), "\n")
	for _, unsafe := range []string{"\x1b[2J", "\x1b]0;title\x07"} {
		if strings.Contains(got, unsafe) {
			t.Fatalf("tool output contains executable terminal control %q: %q", unsafe, got)
		}
	}
	for _, visible := range []string{`\x1b[2Jclear`, `\x1b]0;title\x07`} {
		if !strings.Contains(got, visible) {
			t.Fatalf("tool output missing visible control sequence %q: %q", visible, got)
		}
	}
}

func TestChatPlanCallbacksPrintPlanOnceAndUpdateActivity(t *testing.T) {
	sess, output, messages := newPhase3CallbackSession()
	plan := &agent.Plan{Steps: agent.PlanSteps{
		{Step: "inspect inputs"},
		{Step: "implement fix"},
	}}
	runCtx := &agent.Context{Plan: plan}

	sess.onPlanStepUpdate(runCtx, agent.PlanStepUpdate{
		CompletedIndex: -1,
		StartedIndex:   0,
		StartedStep:    "inspect inputs",
		Reason:         "plan_created",
	})
	sess.onPlanStepUpdate(runCtx, agent.PlanStepUpdate{
		CompletedIndex: 0,
		CompletedStep:  "inspect inputs",
		StartedIndex:   1,
		StartedStep:    "implement fix",
		Reason:         "tool_success",
	})

	if got := strings.Count(output.String(), "Plan\n"); got != 1 {
		t.Fatalf("full plan count = %d, output = %q", got, output.String())
	}
	for _, want := range []string{"1. inspect inputs", "2. implement fix"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("plan output missing %q: %q", want, output.String())
		}
	}
	last := (*messages)[len(*messages)-1]
	activity, ok := last.(thinkingMsg)
	if !ok || activity.message != "plan 2/2 · implement fix" {
		t.Fatalf("current plan activity = %#v", last)
	}

	sess.onPlanStepUpdate(runCtx, agent.PlanStepUpdate{
		CompletedIndex: 1,
		CompletedStep:  "implement fix",
		StartedIndex:   -1,
		Reason:         "tool_success",
	})
	if got := strings.Count(ansi.Strip(output.String()), "✓ Plan complete · 2 steps"); got != 1 {
		t.Fatalf("plan completion summary count = %d, output = %q", got, output.String())
	}
}

func TestChatPlanCallbacksEscapeTerminalControlCharacters(t *testing.T) {
	sess, output, messages := newPhase3CallbackSession()
	plan := &agent.Plan{Steps: agent.PlanSteps{
		{Step: "inspect \x1b[2J inputs"},
	}}
	runCtx := &agent.Context{Plan: plan}

	sess.onPlanStepUpdate(runCtx, agent.PlanStepUpdate{
		CompletedIndex: -1,
		StartedIndex:   0,
		StartedStep:    "inspect \x1b]0;title\x07 inputs",
		Reason:         "plan_created",
	})

	if strings.Contains(output.String(), "\x1b[2J") {
		t.Fatalf("plan transcript contains executable terminal control: %q", output.String())
	}
	if !strings.Contains(output.String(), `inspect \x1b[2J inputs`) {
		t.Fatalf("plan transcript missing visible control sequence: %q", output.String())
	}

	last := (*messages)[len(*messages)-1]
	activity, ok := last.(thinkingMsg)
	if !ok {
		t.Fatalf("plan activity message = %#v", last)
	}
	if strings.Contains(activity.message, "\x1b]0;title\x07") {
		t.Fatalf("plan activity contains executable terminal control: %q", activity.message)
	}
	if !strings.Contains(activity.message, `\x1b]0;title\x07`) {
		t.Fatalf("plan activity missing visible control sequence: %q", activity.message)
	}
}
