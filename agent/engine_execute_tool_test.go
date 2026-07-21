package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/quailyquaily/mistermorph/guard"
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
	observation, err := engine.executeTool(context.Background(), &engineLoopState{}, 0, &ToolCall{Name: "structured"})
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
	observation, err := engine.executeTool(context.Background(), &engineLoopState{}, 0, &ToolCall{Name: "plain"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "partial output\n\nerror: boom"
	if observation != want {
		t.Fatalf("observation = %q, want %q", observation, want)
	}
}

type networkPolicyProbeTool struct {
	hasPolicy bool
}

func (*networkPolicyProbeTool) Name() string            { return "url_fetch" }
func (*networkPolicyProbeTool) Description() string     { return "network policy probe" }
func (*networkPolicyProbeTool) ParameterSchema() string { return `{}` }
func (t *networkPolicyProbeTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	_, t.hasPolicy = guard.NetworkPolicyFromContext(ctx)
	return "ok", nil
}

func TestExecuteURLFetchPassesGuardNetworkPolicyWithAuthProfile(t *testing.T) {
	probe := &networkPolicyProbeTool{}
	reg := tools.NewRegistry()
	if err := reg.Register(probe); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	engine := New(nil, reg, Config{}, DefaultPromptSpec(), WithGuard(guard.New(guard.Config{
		Enabled: true,
		Network: guard.NetworkConfig{URLFetch: guard.URLFetchNetworkPolicy{
			AllowedURLPrefixes: []string{"https://example.test/"},
		}},
	}, nil, nil)))

	_, err := engine.executeTool(context.Background(), &engineLoopState{}, 0, &ToolCall{
		Name: "url_fetch",
		Params: map[string]any{
			"url":          "https://example.test/resource",
			"auth_profile": "profile",
		},
	})
	if err != nil {
		t.Fatalf("executeTool() error = %v", err)
	}
	if !probe.hasPolicy {
		t.Fatal("url_fetch did not receive Guard network policy when auth_profile was set")
	}
}
