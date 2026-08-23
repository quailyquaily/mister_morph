package chatcommands

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		input   string
		wantCmd string
		wantArg string
	}{
		{"/help", "/help", ""},
		{"/say hello world", "/say", "hello world"},
		{"  /models   set foo  ", "/models", "set foo"},
		{"plain text", "plain", "text"},
		{"", "", ""},
		{"/quit", "/quit", ""},
		{"/cmd\nwith newline", "/cmd", "with newline"},
	}

	for _, c := range cases {
		cmd, args := ParseCommand(c.input)
		if cmd != c.wantCmd || args != c.wantArg {
			t.Errorf("ParseCommand(%q) = (%q, %q), want (%q, %q)", c.input, cmd, args, c.wantCmd, c.wantArg)
		}
	}
}

func TestNormalizeCommand(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/help", "/help"},
		{"/Help", "/help"},
		{"/help@MyBot", "/help"},
		{"/models@bot123", "/models"},
		{"plain", ""},
		{"", ""},
		{"  /help  ", "/help"},
	}

	for _, c := range cases {
		got := NormalizeCommand(c.input)
		if got != c.want {
			t.Errorf("NormalizeCommand(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestRegistryRegisterAndDispatch(t *testing.T) {
	r := NewRegistry()

	called := false
	r.Register("/ping", "", func(ctx context.Context, args string) (*Result, error) {
		called = true
		return &Result{Reply: "pong: " + args}, nil
	})

	res, handled, err := r.Dispatch(context.Background(), "/ping hello")
	if !handled {
		t.Fatal("expected handled")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || res.Reply != "pong: hello" {
		t.Fatalf("unexpected reply: %v", res)
	}
	if !called {
		t.Fatal("handler not called")
	}

	_, handled, _ = r.Dispatch(context.Background(), "/unknown")
	if handled {
		t.Fatal("expected not handled for unknown command")
	}
}

func TestRegistryCommandsExposeDescriptions(t *testing.T) {
	r := NewRegistry()
	r.Register("/workspace", "show or change workspace", nil)
	r.Register("/status", "show session details", nil)

	commands := r.Commands()
	if len(commands) != 2 {
		t.Fatalf("Commands() = %#v, want 2 entries", commands)
	}
	if commands[0].Name != "/status" || commands[0].Description != "show session details" {
		t.Fatalf("Commands()[0] = %#v", commands[0])
	}
	if commands[1].Name != "/workspace" || commands[1].Description != "show or change workspace" {
		t.Fatalf("Commands()[1] = %#v", commands[1])
	}
}

func TestRegistryDispatchWithBotSuffix(t *testing.T) {
	r := NewRegistry()
	r.Register("/help", "", func(ctx context.Context, args string) (*Result, error) {
		return &Result{Reply: "help"}, nil
	})

	res, handled, err := r.Dispatch(context.Background(), "/help@MyBot")
	if !handled || err != nil || res == nil || res.Reply != "help" {
		t.Fatalf("unexpected result: %v, %v, %v", res, handled, err)
	}
}

func TestExtractThinkTask(t *testing.T) {
	cases := []struct {
		input    string
		wantTask string
		wantOK   bool
	}{
		{"/think solve this", "solve this", true},
		{"  /think@Morph   solve this  ", "solve this", true},
		{"/think", "", true},
		{"/models list", "/models list", false},
		{"plain text", "plain text", false},
	}
	for _, c := range cases {
		gotTask, gotOK := ExtractThinkTask(c.input)
		if gotTask != c.wantTask || gotOK != c.wantOK {
			t.Fatalf("ExtractThinkTask(%q) = (%q, %v), want (%q, %v)", c.input, gotTask, gotOK, c.wantTask, c.wantOK)
		}
	}
}

func TestIsContextCompactCommand(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{input: "/ctx compact", want: true},
		{input: "  /CTX@Morph   COMPACT  ", want: true},
		{input: "/ctx", want: false},
		{input: "/ctx compact now", want: false},
		{input: "ctx compact", want: false},
	}
	for _, tc := range cases {
		if got := IsContextCompactCommand(tc.input); got != tc.want {
			t.Fatalf("IsContextCompactCommand(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestContextCommandHandlerRequestsCompaction(t *testing.T) {
	statusCalls := 0
	reg := NewRuntimeRegistry(RuntimeRegistryOptions{
		ContextCommand: func() (string, error) {
			statusCalls++
			return "context status", nil
		},
	})

	result, handled, err := reg.Dispatch(context.Background(), "/ctx compact")
	if err != nil {
		t.Fatalf("/ctx compact error = %v", err)
	}
	if !handled || result == nil || result.Action != ActionContextCompact {
		t.Fatalf("/ctx compact result = %#v, handled = %v", result, handled)
	}
	if statusCalls != 0 {
		t.Fatalf("context status calls = %d, want 0", statusCalls)
	}
}

func TestRegistryHandlerError(t *testing.T) {
	r := NewRegistry()
	r.Register("/fail", "", func(ctx context.Context, args string) (*Result, error) {
		return nil, errors.New("boom")
	})

	_, handled, err := r.Dispatch(context.Background(), "/fail")
	if !handled {
		t.Fatal("expected handled")
	}
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected boom error, got: %v", err)
	}
}

func TestRegistryNames(t *testing.T) {
	r := NewRegistry()
	r.Register("/zebra", "", nil)
	r.Register("/apple", "", nil)
	r.Register("/mango", "", nil)

	names := r.Names()
	want := "/apple,/mango,/zebra"
	got := strings.Join(names, ",")
	if got != want {
		t.Fatalf("Names() = %q, want %q", got, want)
	}
}

func TestHelpHandler(t *testing.T) {
	r := NewRegistry()
	r.Register("/help", "", nil)
	r.Register("/models", "", nil)

	h := HelpHandler(r, "Commands:")
	res, err := h(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	reply := res.Reply
	if !strings.Contains(reply, "Commands:") {
		t.Fatalf("expected header in reply: %q", reply)
	}
	if !strings.Contains(reply, "/models") || !strings.Contains(reply, "/help") {
		t.Fatalf("expected command list in reply: %q", reply)
	}
}

func TestHelpHandlerPreservesHeaderNewline(t *testing.T) {
	r := NewRegistry()
	r.Register("/help", "", nil)

	h := HelpHandler(r, "Commands:\n")
	res, err := h(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := res.Reply, "Commands:\n\n  /help"; got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

func TestModelCommandHandlerRebuildsCommandText(t *testing.T) {
	var gotText string
	h := ModelCommandHandler(func(text string) (string, bool, error) {
		gotText = text
		return "ok", true, nil
	})

	res, err := h(context.Background(), "set cheap")
	if err != nil {
		t.Fatalf("ModelCommandHandler() error = %v", err)
	}
	if gotText != "/models set cheap" {
		t.Fatalf("model command text = %q, want %q", gotText, "/models set cheap")
	}
	if res == nil || res.Reply != "ok" {
		t.Fatalf("unexpected reply: %#v", res)
	}
}

func TestRuntimeRegistryHandlesSkillCommand(t *testing.T) {
	reg := NewRuntimeRegistry(RuntimeRegistryOptions{
		SkillCommand: func() (string, error) {
			return "skills ok", nil
		},
	})

	help, handled, err := reg.Dispatch(context.Background(), "/help")
	if err != nil {
		t.Fatalf("/help error = %v", err)
	}
	if !handled || help == nil || !strings.Contains(help.Reply, "/skills") {
		t.Fatalf("/help missing /skills: %#v handled=%v", help, handled)
	}

	res, handled, err := reg.Dispatch(context.Background(), "/skills")
	if err != nil {
		t.Fatalf("/skills error = %v", err)
	}
	if !handled || res == nil || res.Reply != "skills ok" {
		t.Fatalf("unexpected /skills result: %#v handled=%v", res, handled)
	}
}

func TestRuntimeRegistryListsThinkWithoutHandlingIt(t *testing.T) {
	reg := NewRuntimeRegistry(RuntimeRegistryOptions{})
	help, handled, err := reg.Dispatch(context.Background(), "/help")
	if err != nil {
		t.Fatalf("/help error = %v", err)
	}
	if !handled || help == nil || !strings.Contains(help.Reply, "/think") {
		t.Fatalf("/help missing /think: %#v handled=%v", help, handled)
	}
	_, handled, err = reg.Dispatch(context.Background(), "/think use the think route")
	if err != nil {
		t.Fatalf("/think dispatch error = %v", err)
	}
	if handled {
		t.Fatal("expected /think to fall through to agent task execution")
	}
}

func TestRuntimeRegistryHandlesCustomWorkspaceCommand(t *testing.T) {
	var gotArgs string
	reg := NewRuntimeRegistry(RuntimeRegistryOptions{
		WorkspaceCommand: func(args string) (string, error) {
			gotArgs = args
			return "workspace ok", nil
		},
	})

	res, handled, err := reg.Dispatch(context.Background(), "/workspace attach /tmp/project")
	if err != nil {
		t.Fatalf("/workspace error = %v", err)
	}
	if !handled || res == nil || res.Reply != "workspace ok" {
		t.Fatalf("unexpected /workspace result: %#v handled=%v", res, handled)
	}
	if gotArgs != "attach /tmp/project" {
		t.Fatalf("workspace args = %q, want %q", gotArgs, "attach /tmp/project")
	}
}
