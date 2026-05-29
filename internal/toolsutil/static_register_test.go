package toolsutil

import (
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

func TestExplicitBuiltinToolRefs(t *testing.T) {
	refs := ExplicitBuiltinToolRefs("use $bash and $missing", map[string]bool{"bash": true})
	if len(refs) != 0 {
		t.Fatalf("refs = %#v, want none", refs)
	}

	refs = ExplicitBuiltinToolRefs("use $bash and $write_file", nil)
	if len(refs) != 2 || !refs["bash"] || !refs["write_file"] {
		t.Fatalf("refs = %#v, want bash and write_file", refs)
	}

	refs = ExplicitBuiltinToolRefs("use $tool:bash", nil)
	if len(refs) != 0 {
		t.Fatalf("refs = %#v, want none", refs)
	}
}

func TestBuiltinToolTriggersMergesExplicitRefsAndImageIntent(t *testing.T) {
	triggers := BuiltinToolTriggers("$bash 生成图片", nil)
	if !triggers[BuiltinBash] || !triggers[BuiltinImageGenerate] || !triggers[BuiltinImageEdit] {
		t.Fatalf("triggers = %#v, want bash and image tools", triggers)
	}
}

func TestRegisterStaticToolsExplicitEnablesDisabledTool(t *testing.T) {
	reg := tools.NewRegistry()
	RegisterStaticTools(reg, StaticRegistryConfig{
		WriteFile: StaticWriteFileConfig{Enabled: false, MaxBytes: 1024},
	}, nil, map[string]bool{BuiltinWriteFile: true})

	if _, ok := reg.Get(BuiltinWriteFile); !ok {
		t.Fatalf("write_file not registered")
	}
}

func TestRegisterStaticToolsExplicitDoesNotBypassSelectedTools(t *testing.T) {
	reg := tools.NewRegistry()
	RegisterStaticTools(reg, StaticRegistryConfig{
		WriteFile: StaticWriteFileConfig{Enabled: false, MaxBytes: 1024},
	}, map[string]bool{BuiltinBash: true}, map[string]bool{BuiltinWriteFile: true})

	if _, ok := reg.Get(BuiltinWriteFile); ok {
		t.Fatalf("write_file registered despite selected allowlist")
	}
}

func TestRegisterStaticToolsContactsSendEnabledOnlyInAwareness(t *testing.T) {
	reg := tools.NewRegistry()
	RegisterStaticTools(reg, StaticRegistryConfig{
		ContactsSend: StaticContactsSendConfig{Enabled: true},
	}, nil, nil)

	if _, ok := reg.Get(BuiltinContactsSend); ok {
		t.Fatalf("contacts_send registered outside awareness")
	}

	reg = tools.NewRegistry()
	RegisterStaticTools(reg, StaticRegistryConfig{
		Common:       StaticCommonConfig{Awareness: true},
		ContactsSend: StaticContactsSendConfig{Enabled: true},
	}, nil, nil)

	if _, ok := reg.Get(BuiltinContactsSend); !ok {
		t.Fatalf("contacts_send not registered in awareness")
	}
}

func TestRegisterStaticToolsContactsSendDisabledRequiresExplicitTrigger(t *testing.T) {
	reg := tools.NewRegistry()
	RegisterStaticTools(reg, StaticRegistryConfig{
		Common:       StaticCommonConfig{Awareness: true},
		ContactsSend: StaticContactsSendConfig{Enabled: false},
	}, nil, nil)

	if _, ok := reg.Get(BuiltinContactsSend); ok {
		t.Fatalf("disabled contacts_send registered without explicit trigger")
	}

	reg = tools.NewRegistry()
	RegisterStaticTools(reg, StaticRegistryConfig{
		ContactsSend: StaticContactsSendConfig{Enabled: false},
	}, nil, map[string]bool{BuiltinContactsSend: true})

	if _, ok := reg.Get(BuiltinContactsSend); !ok {
		t.Fatalf("explicit contacts_send trigger did not register tool")
	}
}
