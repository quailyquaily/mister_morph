package toolsutil

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/shellenv"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
)

func TestPatchBashInjectedEnvOverridesWithoutMutatingBaseRegistry(t *testing.T) {
	base := tools.NewRegistry()
	baseTool := builtin.NewBashTool(true, 0, 0, pathroots.PathRoots{})
	baseTool.InjectedEnvVars = []shellenv.InjectedEnvVar{{Name: "API_KEY", Value: "global"}}
	base.Register(baseTool)

	clone := tools.NewRegistry()
	for _, tool := range base.All() {
		clone.Register(tool)
	}

	if err := PatchBashInjectedEnv(clone, []shellenv.InjectedEnvVar{{Name: "API_KEY", Value: "task"}}); err != nil {
		t.Fatalf("PatchBashInjectedEnv() error = %v", err)
	}

	clonedTool, ok := clone.Get(BuiltinBash)
	if !ok {
		t.Fatal("cloned bash tool missing")
	}
	clonedBash := clonedTool.(*builtin.BashTool)
	if len(clonedBash.InjectedEnvVars) != 1 || clonedBash.InjectedEnvVars[0].Value != "task" {
		t.Fatalf("cloned injected = %#v", clonedBash.InjectedEnvVars)
	}

	baseToolAfter, _ := base.Get(BuiltinBash)
	baseBash := baseToolAfter.(*builtin.BashTool)
	if len(baseBash.InjectedEnvVars) != 1 || baseBash.InjectedEnvVars[0].Value != "global" {
		t.Fatalf("base injected mutated = %#v", baseBash.InjectedEnvVars)
	}
}
