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
	r.Register("/start", func(ctx context.Context, args string) (*Result, error) {
		return &Result{Reply: "started"}, nil
	})

	res, handled, err := r.Dispatch(context.Background(), "/start@MyBot")
	if !handled || err != nil || res == nil || res.Reply != "started" {
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
	r.Register("/echo", nil)

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
	if !strings.Contains(reply, "/echo") || !strings.Contains(reply, "/help") {
		t.Fatalf("expected command list in reply: %q", reply)
	}
}

func TestEchoHandler(t *testing.T) {
	h := EchoHandler()
	res, err := h(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || res.Reply != "hello world" {
		t.Fatalf("unexpected reply: %v", res)
	}

	res, err = h(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || !strings.Contains(res.Reply, "usage") {
		t.Fatalf("expected usage hint for empty args, got: %v", res)
	}
}
