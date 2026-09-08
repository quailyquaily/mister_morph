package configdefaults

import (
	"bytes"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/assets"
	"github.com/spf13/viper"
)

func TestRuntimeDefaultsMatchConfigTemplate(t *testing.T) {
	raw, err := assets.ConfigFS.ReadFile("config/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	template := viper.New()
	template.SetConfigType("yaml")
	if err := template.ReadConfig(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	defaults := viper.New()
	Apply(defaults)
	for key, want := range map[string]int{
		"max_steps": 1024, "parse_retries": 16, "tool_repeat_limit": 256, "max_token_budget": 0,
	} {
		t.Run(key, func(t *testing.T) {
			if got := defaults.GetInt(key); got != want {
				t.Errorf("default = %d, want %d", got, want)
			}
			if got := template.GetInt(key); got != want {
				t.Errorf("template = %d, want %d", got, want)
			}
			defaults.Set(key, 3)
			Apply(defaults)
			if got := defaults.GetInt(key); got != 3 {
				t.Errorf("explicit value = %d, want 3", got)
			}
		})
	}
	for key, want := range map[string]time.Duration{
		"timeout":                  time.Hour,
		"tools.web_search.timeout": time.Minute,
		"tools.bash.timeout":       time.Minute,
		"tools.powershell.timeout": time.Minute,
	} {
		t.Run(key, func(t *testing.T) {
			if got := defaults.GetDuration(key); got != want {
				t.Errorf("default = %s, want %s", got, want)
			}
			if got := template.GetDuration(key); got != want {
				t.Errorf("template = %s, want %s", got, want)
			}
			defaults.Set(key, "7s")
			Apply(defaults)
			if got := defaults.GetDuration(key); got != 7*time.Second {
				t.Errorf("explicit value = %s, want 7s", got)
			}
		})
	}
}

func TestApplySetsContextCompactionDefaults(t *testing.T) {
	v := viper.New()
	Apply(v)
	if !v.GetBool("context_compaction.enabled") {
		t.Fatal("context_compaction.enabled = false, want true")
	}
	if got := v.GetFloat64("context_compaction.trigger_ratio"); got != 0.80 {
		t.Fatalf("trigger ratio = %v, want 0.80", got)
	}
}

func TestApplySetsEmptyDefaultWorkspaceDir(t *testing.T) {
	v := viper.New()
	Apply(v)
	if got := v.GetString("workspace_dir"); got != "" {
		t.Fatalf("workspace_dir = %q, want empty", got)
	}
}

func TestApplyDisablesRecordUntriggeredByDefault(t *testing.T) {
	v := viper.New()
	Apply(v)
	for _, key := range []string{
		"telegram.record_untriggered",
		"slack.record_untriggered",
		"line.record_untriggered",
		"lark.record_untriggered",
	} {
		if !v.IsSet(key) {
			t.Fatalf("%s has no registered default", key)
		}
		if v.GetBool(key) {
			t.Fatalf("%s = true, want false", key)
		}
	}
}

func TestApplySetsMixinDefaults(t *testing.T) {
	v := viper.New()
	Apply(v)
	if got := v.GetInt("mixin.max_concurrency"); got != DefaultChannelMaxConcurrency {
		t.Fatalf("mixin.max_concurrency = %d, want %d", got, DefaultChannelMaxConcurrency)
	}
	if got := v.GetStringSlice("mixin.allowed_conversation_ids"); len(got) != 0 {
		t.Fatalf("mixin.allowed_conversation_ids = %#v, want empty", got)
	}
}

func TestApplyDoesNotRegisterJournalDirectoryConfig(t *testing.T) {
	v := viper.New()
	Apply(v)
	if v.IsSet("journal.dir_name") {
		t.Fatal("journal.dir_name should not be configurable")
	}
}

func TestApplyDoesNotRegisterRemovedConfig(t *testing.T) {
	v := viper.New()
	Apply(v)
	for _, key := range []string{
		"contacts.proactive.max_turns_per_session",
		"contacts.proactive.session_cooldown",
		"console.enabled",
		"server.listen",
		"submit.wait",
		"submit.poll_interval",
	} {
		if v.IsSet(key) {
			t.Errorf("%s should not be configurable", key)
		}
	}
}
