package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

type executeToolStub struct {
	name string
	out  string
	err  error
}

func (t *executeToolStub) Name() string            { return t.name }
func (t *executeToolStub) Description() string     { return "execute tool stub" }
func (t *executeToolStub) ParameterSchema() string { return "{}" }
func (t *executeToolStub) Execute(_ context.Context, _ map[string]any) (string, error) {
	return t.out, t.err
}

func TestExecuteTool_PreservesStructuredObservationOnError(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&executeToolStub{
		name: "structured",
		out:  `{"status":"failed","error":"boom"}`,
		err:  tools.PreserveObservationError(errors.New("boom")),
	})

	engine := New(nil, reg, Config{}, DefaultPromptSpec())
	observation, err := engine.executeTool(context.Background(), &engineLoopState{}, &ToolCall{Name: "structured"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if observation != `{"status":"failed","error":"boom"}` {
		t.Fatalf("observation = %q, want unchanged JSON envelope", observation)
	}
}

func TestExecuteTool_AppendsErrorForPlainObservation(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&executeToolStub{
		name: "plain",
		out:  "partial output",
		err:  errors.New("boom"),
	})

	engine := New(nil, reg, Config{}, DefaultPromptSpec())
	observation, err := engine.executeTool(context.Background(), &engineLoopState{}, &ToolCall{Name: "plain"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "partial output\n\nerror: boom"
	if observation != want {
		t.Fatalf("observation = %q, want %q", observation, want)
	}
}
