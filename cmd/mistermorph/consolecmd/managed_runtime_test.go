package consolecmd

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

func TestManagedRuntimeSupervisorReloadSkipsSlackWithoutTokens(t *testing.T) {
	cases := []struct {
		name    string
		setNext func(*viper.Viper)
	}{
		{name: "missing bot token"},
		{
			name: "missing app token",
			setNext: func(v *viper.Viper) {
				v.Set("slack.bot_token", "xoxb-test")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			local := &consoleLocalRuntime{managedRuntimeRunning: map[string]bool{}}
			local.SetManagedRuntimeRunning("slack", true)
			supervisor := newManagedRuntimeSupervisor(local, false, false)
			supervisor.parentCtx = context.Background()
			supervisor.configReader = viper.New()
			supervisor.kinds = []string{"slack"}

			next := viper.New()
			next.Set("console.managed_runtimes", []string{"slack"})
			if tc.setNext != nil {
				tc.setNext(next)
			}

			err := supervisor.ReloadConfig(next)
			if err != nil {
				t.Fatalf("ReloadConfig() error = %v, want nil", err)
			}
			if supervisor.configReader != next {
				t.Fatal("configReader was not updated")
			}
			if len(supervisor.kinds) != 1 || supervisor.kinds[0] != "slack" {
				t.Fatalf("kinds = %#v, want [slack]", supervisor.kinds)
			}
			if local.isManagedRuntimeRunning("slack") {
				t.Fatal("slack running = true, want disabled")
			}
		})
	}
}

func TestManagedRuntimeSupervisorReloadSkipsTelegramWithoutToken(t *testing.T) {
	local := &consoleLocalRuntime{managedRuntimeRunning: map[string]bool{}}
	local.SetManagedRuntimeRunning("telegram", true)
	supervisor := newManagedRuntimeSupervisor(local, false, false)
	supervisor.parentCtx = context.Background()
	supervisor.configReader = viper.New()
	supervisor.kinds = []string{"telegram"}

	next := viper.New()
	next.Set("console.managed_runtimes", []string{"telegram"})

	err := supervisor.ReloadConfig(next)
	if err != nil {
		t.Fatalf("ReloadConfig() error = %v, want nil", err)
	}
	if supervisor.configReader != next {
		t.Fatal("configReader was not updated")
	}
	if len(supervisor.kinds) != 1 || supervisor.kinds[0] != "telegram" {
		t.Fatalf("kinds = %#v, want [telegram]", supervisor.kinds)
	}
	if local.isManagedRuntimeRunning("telegram") {
		t.Fatal("telegram running = true, want disabled")
	}
}

func TestManagedRuntimeKindsFromReaderRejectsUnsupportedValue(t *testing.T) {
	v := viper.New()
	v.Set("console.managed_runtimes", []string{"telegram", "line"})

	_, err := managedRuntimeKindsFromReader(v)
	if err == nil || err.Error() == "" {
		t.Fatalf("managedRuntimeKindsFromReader() error = %v, want unsupported value", err)
	}
}
