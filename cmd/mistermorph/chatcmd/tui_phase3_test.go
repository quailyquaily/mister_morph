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
	if !ok || !activity.on || activity.message != `bash · {"cmd":"go test ./...","timeout":30}` {
		t.Fatalf("tool start activity = %#v", (*messages)[0])
	}

	sess.onToolCallDone(nil, call, "ok", nil)
	plainOutput := ansi.Strip(output.String())
	if got := strings.Count(plainOutput, `✓ bash · {"cmd":"go test ./...","timeout":30}`); got != 1 {
		t.Fatalf("tool completion summary count = %d, output = %q", got, output.String())
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
	for _, want := range []string{`× bash · {"cmd":"false","timeout":30}`, "exit status 1"} {
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

func TestChatActivityNormalizationPreservesParameterSpacing(t *testing.T) {
	t.Parallel()

	input := `bash · {"cmd":"printf 'a  b'"}`
	if got := normalizeActivityText(input); got != input {
		t.Fatalf("normalizeActivityText() = %q, want %q", got, input)
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
