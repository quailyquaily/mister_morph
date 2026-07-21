package runcmd

import (
	"strconv"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
)

func TestRunFlagDefaultsMatchConfigDefaults(t *testing.T) {
	cmd := New(Dependencies{})

	want := map[string]string{
		"provider":            "",
		"endpoint":            "",
		"model":               "",
		"llm-request-timeout": configdefaults.DefaultLLMRequestTimeout.String(),
		"max-steps":           strconv.Itoa(configdefaults.DefaultMaxSteps),
		"parse-retries":       strconv.Itoa(configdefaults.DefaultParseRetries),
		"max-token-budget":    strconv.FormatInt(configdefaults.DefaultMaxTokenBudget, 10),
		"tool-repeat-limit":   strconv.Itoa(configdefaults.DefaultToolRepeatLimit),
		"timeout":             configdefaults.DefaultTaskTimeout.String(),
	}
	for name, expected := range want {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("missing flag %q", name)
		}
		if flag.DefValue != expected {
			t.Errorf("--%s default = %q, want %q", name, flag.DefValue, expected)
		}
	}
}
