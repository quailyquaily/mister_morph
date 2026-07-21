package linecmd

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

func TestBuildLineRuntimeDepsPreservesCommonCapabilities(t *testing.T) {
	t.Parallel()

	base := depsutil.CommonDependencies{
		ResolveLLMRouteWithProfile: func(string, string) (llmutil.ResolvedRoute, error) { return llmutil.ResolvedRoute{}, nil },
		AwarenessRegistry:          tools.NewRegistry,
		ToolTriggers:               func(string) map[string]bool { return map[string]bool{"sentinel": true} },
		RegisterTriggeredStaticTools: func(*tools.Registry, map[string]bool) {
		},
		ACPAgents: func() []acpclient.AgentConfig { return []acpclient.AgentConfig{{Name: "sentinel"}} },
	}
	got := buildLineRuntimeDeps(Dependencies{Dependencies: base}, toolsutil.RuntimeToolsRegisterConfig{}, viper.New()).CommonDependencies
	if got.ResolveLLMRouteWithProfile == nil || got.AwarenessRegistry == nil || got.ToolTriggers == nil || got.RegisterTriggeredStaticTools == nil || got.ACPAgents == nil {
		t.Fatalf("common dependency capability was dropped: %#v", got)
	}
}
