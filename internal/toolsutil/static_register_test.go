package toolsutil

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/spf13/viper"
)

func TestStaticRegistryConfigFromReaderOwnsStaticToolConfiguration(t *testing.T) {
	reader := viper.New()
	stateDir := t.TempDir()
	cacheDir := t.TempDir()
	reader.Set("file_state_dir", stateDir)
	reader.Set("file_cache_dir", cacheDir)
	reader.Set("contacts.dir_name", "people")
	reader.Set("tools.bash.rewrite.enabled", true)
	reader.Set("tools.bash.rewrite.binary", "sandbox-shell")
	reader.Set("tools.bash.injected_env_vars", []map[string]any{{"name": "TOKEN", "value": "secret"}})
	reader.Set("tools.contacts_send.enabled", true)
	reader.Set("lark.app_id", "lark-id")
	reader.Set("lark.app_secret", "lark-secret")
	reader.Set("lark.base_url", "https://open.example.test")
	reader.Set("secrets.allow_profiles", []string{"billing"})
	reader.Set("auth_profiles", map[string]any{
		"billing": map[string]any{
			"credential": map[string]any{"kind": "api_key", "secret": "token"},
			"allow": map[string]any{
				"url_prefixes": []string{"https://api.example.test/v1"},
				"methods":      []string{"GET"},
			},
			"bindings": map[string]any{
				"url_fetch": map[string]any{
					"inject": map[string]any{"location": "header", "name": "Authorization", "format": "bearer"},
				},
			},
		},
	})

	cfg, err := StaticRegistryConfigFromReader(reader)
	if err != nil {
		t.Fatalf("StaticRegistryConfigFromReader() error = %v", err)
	}
	if !cfg.Bash.Rewrite.Enabled || cfg.Bash.Rewrite.Binary != "sandbox-shell" {
		t.Fatalf("bash rewrite = %#v", cfg.Bash.Rewrite)
	}
	if len(cfg.Bash.InjectedEnvVars) != 1 || cfg.Bash.InjectedEnvVars[0].Name != "TOKEN" {
		t.Fatalf("bash injected env = %#v", cfg.Bash.InjectedEnvVars)
	}
	if cfg.ContactsSend.LarkAppID != "lark-id" || cfg.ContactsSend.LarkAppSecret != "lark-secret" || cfg.ContactsSend.LarkBaseURL != "https://open.example.test" {
		t.Fatalf("lark contacts credentials = %#v", cfg.ContactsSend)
	}
	if cfg.ContactsSend.ContactsDir != filepath.Join(stateDir, "people") {
		t.Fatalf("contacts dir = %q", cfg.ContactsSend.ContactsDir)
	}
	if cfg.Common.PathRoots.FileCacheDir != cacheDir || cfg.Common.PathRoots.FileStateDir != stateDir {
		t.Fatalf("path roots = %#v", cfg.Common.PathRoots)
	}
	if !cfg.Common.AuthenticatedHTTPConfigured || cfg.URLFetch.Auth == nil {
		t.Fatalf("auth config = %#v", cfg.URLFetch.Auth)
	}
	profile, ok := cfg.URLFetch.Auth.Profiles.Get("billing")
	if !ok || profile.ID != "billing" || len(profile.Allow.ParsedURLPrefixes) != 1 {
		t.Fatalf("validated auth profile = %#v, found = %v", profile, ok)
	}
}

func TestStaticRegistryConfigFromReaderRejectsInvalidAuthProfile(t *testing.T) {
	reader := viper.New()
	reader.Set("auth_profiles", map[string]any{
		"broken": map[string]any{
			"credential": map[string]any{"kind": "api_key", "secret": "token"},
		},
	})

	_, err := StaticRegistryConfigFromReader(reader)
	if err == nil || !strings.Contains(err.Error(), "auth_profiles.broken") {
		t.Fatalf("StaticRegistryConfigFromReader() error = %v, want auth profile validation error", err)
	}
}

func TestStaticRegistryConfigFromReaderNormalizesAuthProfileIDs(t *testing.T) {
	reader := viper.New()
	reader.Set("secrets.allow_profiles", []string{"billing"})
	reader.Set("auth_profiles", map[string]any{
		" billing ": map[string]any{
			"credential": map[string]any{"kind": "api_key", "secret": "token"},
			"allow": map[string]any{
				"url_prefixes": []string{"https://api.example.test/v1"},
				"methods":      []string{"GET"},
			},
			"bindings": map[string]any{
				"url_fetch": map[string]any{
					"inject": map[string]any{"location": "header", "name": "Authorization", "format": "bearer"},
				},
			},
		},
	})

	cfg, err := StaticRegistryConfigFromReader(reader)
	if err != nil {
		t.Fatalf("StaticRegistryConfigFromReader() error = %v", err)
	}
	if !cfg.Common.AuthenticatedHTTPConfigured {
		t.Fatal("authenticated HTTP was not detected for normalized profile ID")
	}
	profile, ok := cfg.URLFetch.Auth.Profiles.Get("billing")
	if !ok || profile.ID != "billing" {
		t.Fatalf("normalized auth profile = %#v, found = %v", profile, ok)
	}
}

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

func TestRegisterStaticToolsAgentSendRequiresActiveAgent(t *testing.T) {
	ctx := context.Background()
	contactsDir := filepath.Join(t.TempDir(), "contacts")
	cfg := StaticRegistryConfig{
		ContactsSend: StaticContactsSendConfig{ContactsDir: contactsDir},
	}

	reg := tools.NewRegistry()
	RegisterStaticTools(reg, cfg, nil, nil)
	if _, ok := reg.Get(BuiltinAgentSend); ok {
		t.Fatal("agent_send registered without an active Agent")
	}

	svc := contacts.NewService(contacts.NewFileStore(contactsDir))
	if _, err := svc.UpsertContact(ctx, contacts.Contact{
		ContactID: "contact:alice",
		Kind:      contacts.KindHuman,
		Channel:   contacts.ChannelTelegram,
	}, time.Now().UTC()); err != nil {
		t.Fatalf("UpsertContact(human) error = %v", err)
	}
	reg = tools.NewRegistry()
	RegisterStaticTools(reg, cfg, nil, nil)
	if _, ok := reg.Get(BuiltinAgentSend); ok {
		t.Fatal("agent_send registered with only active humans")
	}

	if _, err := svc.UpsertContact(ctx, contacts.Contact{
		ContactID:  "contact:smith",
		Kind:       contacts.KindAgent,
		Channel:    contacts.ChannelTelegram,
		TGUsername: "smith_bot",
	}, time.Now().UTC()); err != nil {
		t.Fatalf("UpsertContact(agent) error = %v", err)
	}
	reg = tools.NewRegistry()
	RegisterStaticTools(reg, cfg, nil, nil)
	if _, ok := reg.Get(BuiltinAgentSend); !ok {
		t.Fatal("agent_send not registered with an active Agent")
	}
	if _, ok := reg.Get(BuiltinContactsSend); ok {
		t.Fatal("agent_send availability changed contacts_send registration")
	}
}
