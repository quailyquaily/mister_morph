package integration

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/tools"
)

func TestSharedDependenciesRetainCommonCapabilities(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Set("tools.write_file.enabled", false)
	cfg.Set("tasks.persistence_targets", []string{"telegram"})
	cfg.Set("tasks.rotate_max_bytes", int64(12345))
	rt := New(cfg)
	snap := rt.snapshot()
	var common depsutil.CommonDependencies = rt.sharedDependencies(snap)

	if common.ResolveLLMRouteWithProfile == nil {
		t.Fatal("ResolveLLMRouteWithProfile was dropped")
	}
	if common.CreateImageClient == nil {
		t.Fatal("CreateImageClient was dropped")
	}
	if common.AwarenessRegistry == nil {
		t.Fatal("AwarenessRegistry was dropped")
	}
	if common.RegisterTriggeredStaticTools == nil {
		t.Fatal("RegisterTriggeredStaticTools was dropped")
	}
	if err := common.Validate(); err != nil {
		t.Fatalf("common dependency validation failed: %v", err)
	}
	if common.ACPAgents == nil || common.PromptAugment == nil {
		t.Fatal("optional common behavior was dropped")
	}
	if common.RuntimePaths.StateDir == "" || len(common.TaskPersistenceTargets) != 1 || common.TaskPersistenceTargets[0] != "telegram" || common.TaskRotateMaxBytes != 12345 {
		t.Fatalf("runtime state dependencies were dropped: %#v", common)
	}
	if _, ok := common.Registry().Get(toolsutil.BuiltinContactsSend); ok {
		t.Fatal("base registry contains awareness-only contacts_send")
	}
	if _, ok := common.AwarenessRegistry().Get(toolsutil.BuiltinContactsSend); !ok {
		t.Fatal("awareness registry dropped contacts_send")
	}

	telegramDeps := rt.telegramDependencies(snap).CommonDependencies
	slackDeps := rt.slackDependencies(snap).CommonDependencies
	for name, deps := range map[string]depsutil.CommonDependencies{
		"telegram": telegramDeps,
		"slack":    slackDeps,
	} {
		if deps.ResolveLLMRouteWithProfile == nil || deps.CreateImageClient == nil || deps.AwarenessRegistry == nil || deps.RegisterTriggeredStaticTools == nil {
			t.Fatalf("%s dependencies dropped a common capability", name)
		}
		reg := tools.NewRegistry()
		deps.RegisterTriggeredStaticTools(reg, map[string]bool{toolsutil.BuiltinWriteFile: true})
		if _, ok := reg.Get(toolsutil.BuiltinWriteFile); !ok {
			t.Fatalf("%s dependencies did not register a triggered static tool", name)
		}
	}
}
