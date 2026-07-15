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
