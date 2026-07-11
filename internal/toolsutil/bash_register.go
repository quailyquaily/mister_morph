package toolsutil

import (
	"fmt"

	"github.com/quailyquaily/mistermorph/internal/shellenv"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
)

// PatchBashInjectedEnv shallow-copies the registered bash tool and merges extra
// injected env vars so task-level values override same-named global entries.
func PatchBashInjectedEnv(reg *tools.Registry, extra []shellenv.InjectedEnvVar) error {
	if reg == nil || len(extra) == 0 {
		return nil
	}
	tool, ok := reg.Get(BuiltinBash)
	if !ok {
		return fmt.Errorf("bash tool is not registered")
	}
	bashTool, ok := tool.(*builtin.BashTool)
	if !ok {
		return fmt.Errorf("bash tool has unexpected type %T", tool)
	}
	copy := *bashTool
	copy.InjectedEnvVars = shellenv.MergeInjectedEnvVars(bashTool.InjectedEnvVars, extra)
	reg.Register(&copy)
	return nil
}
