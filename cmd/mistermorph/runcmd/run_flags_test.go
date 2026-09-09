package runcmd

import (
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

func TestRunPositionalTask(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      []string
		want      string
		wantError string
	}{
		{name: "quoted", args: []string{"整理今天的日志"}, want: "整理今天的日志"},
		{name: "words", args: []string{"summarize", "today's", "logs"}, want: "summarize today's logs"},
		{name: "with flags", args: []string{"summarize logs", "--model", "test-model"}, want: "summarize logs"},
		{name: "legacy flag", args: []string{"--task", "summarize logs"}, want: "summarize logs"},
		{name: "dash prefix", args: []string{"--", "--explain this"}, want: "--explain this"},
		{name: "preserve formatting", args: []string{"  line one\n  line two  "}, want: "line one\n  line two"},
		{name: "config fallback", args: []string{}, want: "configured task"},
		{name: "two task sources", args: []string{"--task", "first", "second"}, wantError: "--task"},
		{name: "empty flag conflict", args: []string{"--task=", "second"}, wantError: "--task"},
		{name: "heartbeat conflict", args: []string{"--heartbeat", "custom task"}, wantError: "--heartbeat"},
		{name: "heartbeat false", args: []string{"--heartbeat=false", "custom task"}, want: "custom task"},
		{name: "blank task", args: []string{" \n "}, wantError: "empty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			viper.Set("task", "configured task")
			cmd := New(Dependencies{})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)
			ran := false
			cmd.RunE = func(cmd *cobra.Command, _ []string) error {
				ran = true
				if got := strings.TrimSpace(configutil.FlagOrViperString(cmd, "task", "task")); got != tt.want {
					t.Errorf("task = %q, want %q", got, tt.want)
				}
				return nil
			}
			err := cmd.Execute()
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) || ran {
					t.Fatalf("error = %v, ran = %v, want %q before running", err, ran, tt.wantError)
				}
			} else if err != nil || !ran {
				t.Fatalf("error = %v, ran = %v", err, ran)
			}
		})
	}
}
