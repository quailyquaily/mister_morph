package mcphost

import (
	"context"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

type hostTestTool struct {
	name string
}

func (t hostTestTool) Name() string          { return t.name }
func (hostTestTool) Description() string     { return "test tool" }
func (hostTestTool) ParameterSchema() string { return `{}` }
func (hostTestTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

func TestRegisterHostToolsReturnsCollisionAndCleansHost(t *testing.T) {
	reg := tools.NewRegistry()
	original := hostTestTool{name: "duplicate"}
	if err := reg.Register(original); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	host := &Host{tools: []tools.Tool{
		hostTestTool{name: "fresh"},
		hostTestTool{name: "duplicate"},
	}}

	err := RegisterHostTools(host, reg)
	if err == nil {
		t.Fatal("RegisterHostTools() error = nil, want collision error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("RegisterHostTools() error = %q, want tool name", err)
	}
	got, ok := reg.Get("duplicate")
	if !ok || got != original {
		t.Fatalf("registry tool = %v, want original tool", got)
	}
	if got := host.Tools(); len(got) != 0 {
		t.Fatalf("host tools after collision = %v, want closed host", got)
	}
	if _, ok := reg.Get("fresh"); ok {
		t.Fatal("tool registered before collision was not rolled back")
	}
}
