package configdefaults

import (
	"testing"

	"github.com/spf13/viper"
)

func TestApplySetsContextCompactionDefaults(t *testing.T) {
	v := viper.New()
	Apply(v)
	if !v.GetBool("context_compaction.enabled") {
		t.Fatal("context_compaction.enabled = false, want true")
	}
	if got := v.GetFloat64("context_compaction.trigger_ratio"); got != 0.80 {
		t.Fatalf("trigger ratio = %v, want 0.80", got)
	}
	if got := v.GetInt("context_compaction.output_reserve_tokens"); got != 0 {
		t.Fatalf("output reserve = %d, want 0", got)
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
