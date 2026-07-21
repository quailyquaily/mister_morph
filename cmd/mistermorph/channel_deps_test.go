package main

import (
	"context"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/toolsutil"
)

func TestChannelCommandRuntimeSplitsRuntimeAndAwarenessRegistries(t *testing.T) {
	registryResolver := &registryRuntimeResolver{}
	registryResolver.once.Do(func() {
		registryResolver.cfg = registryConfig{
			ContactsSend: toolsutil.StaticContactsSendConfig{
				Enabled:     true,
				ContactsDir: t.TempDir(),
			},
		}
	})
	if err := registryResolver.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	deps := newChannelCommandRuntime().Dependencies(registryResolver, &guardRuntimeResolver{})
	if deps.Registry == nil {
		t.Fatal("Registry is nil")
	}
	if deps.AwarenessRegistry == nil {
		t.Fatal("AwarenessRegistry is nil")
	}
	if deps.ACPAgents == nil {
		t.Fatal("ACPAgents is nil")
	}

	runtimeReg := deps.Registry()
	if _, ok := runtimeReg.Get(toolsutil.BuiltinContactsSend); ok {
		t.Fatalf("runtime registry includes %s", toolsutil.BuiltinContactsSend)
	}

	awarenessReg := deps.AwarenessRegistry()
	if _, ok := awarenessReg.Get(toolsutil.BuiltinContactsSend); !ok {
		t.Fatalf("awareness registry missing %s", toolsutil.BuiltinContactsSend)
	}
}
