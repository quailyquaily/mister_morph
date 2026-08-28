package mixincmd

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

func TestBuildMixinRuntimeDepsPreservesCommonCapabilities(t *testing.T) {
	base := depsutil.CommonDependencies{
		ResolveLLMRouteWithProfile: func(string, string) (llmutil.ResolvedRoute, error) { return llmutil.ResolvedRoute{}, nil },
		AwarenessRegistry:          tools.NewRegistry,
		ToolTriggers:               func(string) map[string]bool { return map[string]bool{"sentinel": true} },
		RegisterTriggeredStaticTools: func(*tools.Registry, map[string]bool) {
		},
		ACPAgents: func() []acpclient.AgentConfig { return []acpclient.AgentConfig{{Name: "sentinel"}} },
	}
	reader := viper.New()
	reader.Set("workspace_dir", "/srv/mistermorph-workspace")
	got := buildMixinRuntimeDeps(Dependencies{Dependencies: base}, toolsutil.RuntimeToolsRegisterConfig{}, reader).CommonDependencies
	if got.ResolveLLMRouteWithProfile == nil || got.AwarenessRegistry == nil || got.ToolTriggers == nil || got.RegisterTriggeredStaticTools == nil || got.ACPAgents == nil {
		t.Fatalf("common dependency capability was dropped: %#v", got)
	}
	if got.DefaultWorkspaceDir != "/srv/mistermorph-workspace" || got.AgentSettingsOwner == nil || got.RuntimeConfigSource == nil {
		t.Fatalf("runtime config dependency was dropped: %#v", got)
	}
}
