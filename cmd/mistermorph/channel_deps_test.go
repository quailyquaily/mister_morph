package main

import (
	"testing"

	"github.com/quailyquaily/mistermorph/internal/toolsutil"
)

func TestChannelCommandRuntimeSplitsRuntimeAndAwarenessRegistries(t *testing.T) {
	registryResolver := &registryRuntimeResolver{}
	registryResolver.once.Do(func() {
		registryResolver.cfg = registryConfig{
			ToolsContactsSendEnabled: true,
			ContactsDir:              t.TempDir(),
		}
	})

	deps := newChannelCommandRuntime().Dependencies(registryResolver, &guardRuntimeResolver{})
	if deps.Registry == nil {
		t.Fatal("Registry is nil")
	}
	if deps.AwarenessRegistry == nil {
		t.Fatal("AwarenessRegistry is nil")
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
