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
		{"  /model   set foo  ", "/model", "set foo"},
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
		{"/model@bot123", "/model"},
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
	r.Register("/ping", func(ctx context.Context, args string) (*Result, error) {
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

func TestRegistryDispatchWithBotSuffix(t *testing.T) {
	r := NewRegistry()
	r.Register("/help", func(ctx context.Context, args string) (*Result, error) {
		return &Result{Reply: "help"}, nil
	})

	res, handled, err := r.Dispatch(context.Background(), "/help@MyBot")
	if !handled || err != nil || res == nil || res.Reply != "help" {
		t.Fatalf("unexpected result: %v, %v, %v", res, handled, err)
	}
}

func TestRegistryHandlerError(t *testing.T) {
	r := NewRegistry()
	r.Register("/fail", func(ctx context.Context, args string) (*Result, error) {
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
	r.Register("/zebra", nil)
	r.Register("/apple", nil)
	r.Register("/mango", nil)

	names := r.Names()
	want := "/apple,/mango,/zebra"
	got := strings.Join(names, ",")
	if got != want {
		t.Fatalf("Names() = %q, want %q", got, want)
	}
}

func TestHelpHandler(t *testing.T) {
	r := NewRegistry()
	r.Register("/help", nil)
	r.Register("/model", nil)

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
	if !strings.Contains(reply, "/model") || !strings.Contains(reply, "/help") {
		t.Fatalf("expected command list in reply: %q", reply)
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
	if gotText != "/model set cheap" {
		t.Fatalf("model command text = %q, want %q", gotText, "/model set cheap")
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
	if !handled || help == nil || !strings.Contains(help.Reply, "/skill") {
		t.Fatalf("/help missing /skill: %#v handled=%v", help, handled)
	}

	res, handled, err := reg.Dispatch(context.Background(), "/skill")
	if err != nil {
		t.Fatalf("/skill error = %v", err)
	}
	if !handled || res == nil || res.Reply != "skills ok" {
		t.Fatalf("unexpected /skill result: %#v handled=%v", res, handled)
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
