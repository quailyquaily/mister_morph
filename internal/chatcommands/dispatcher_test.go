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
		{"/echo hello world", "/echo", "hello world"},
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
		{"  /start  ", "/start"},
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
	r.Register("/ping", func(ctx context.Context, args string) (string, error) {
		called = true
		return "pong: " + args, nil
	})

	reply, handled, err := r.Dispatch(context.Background(), "/ping hello")
	if !handled {
		t.Fatal("expected handled")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "pong: hello" {
		t.Fatalf("unexpected reply: %q", reply)
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
	r.Register("/start", func(ctx context.Context, args string) (string, error) {
		return "started", nil
	})

	reply, handled, err := r.Dispatch(context.Background(), "/start@MyBot")
	if !handled || err != nil || reply != "started" {
		t.Fatalf("unexpected result: %q, %v, %v", reply, handled, err)
	}
}

func TestRegistryHandlerError(t *testing.T) {
	r := NewRegistry()
	r.Register("/fail", func(ctx context.Context, args string) (string, error) {
		return "", errors.New("boom")
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
	r.Register("/echo", nil)

	h := HelpHandler(r, "Commands:")
	reply, err := h(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(reply, "Commands:") {
		t.Fatalf("expected header in reply: %q", reply)
	}
	if !strings.Contains(reply, "/echo") || !strings.Contains(reply, "/help") {
		t.Fatalf("expected command list in reply: %q", reply)
	}
}

func TestEchoHandler(t *testing.T) {
	h := EchoHandler()
	reply, err := h(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "hello world" {
		t.Fatalf("unexpected reply: %q", reply)
	}

	reply, err = h(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(reply, "usage") {
		t.Fatalf("expected usage hint for empty args, got: %q", reply)
	}
}
