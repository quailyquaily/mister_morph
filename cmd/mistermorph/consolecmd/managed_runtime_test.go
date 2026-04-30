package consolecmd

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

func TestManagedRuntimeSupervisorReloadDisablesChannelMissingToken(t *testing.T) {
	local := &consoleLocalRuntime{managedRuntimeRunning: map[string]bool{}}
	local.SetManagedRuntimeRunning("telegram", true)
	supervisor := newManagedRuntimeSupervisor(local, false, false)

	current := viper.New()
	current.Set("console.managed_runtimes", []string{"telegram"})
	current.Set("telegram.bot_token", "old-token")
	supervisor.parentCtx = context.Background()
	supervisor.configReader = current
	supervisor.kinds = []string{"telegram"}

	next := viper.New()
	next.Set("console.managed_runtimes", []string{"telegram"})

	err := supervisor.ReloadConfig(next)
	if err != nil {
		t.Fatalf("ReloadConfig() error = %v, want nil", err)
	}
	if got := supervisor.configReader.GetString("telegram.bot_token"); got != "" {
		t.Fatalf("configReader.telegram.bot_token = %q, want empty", got)
	}
	if len(supervisor.kinds) != 0 {
		t.Fatalf("kinds = %#v, want empty", supervisor.kinds)
	}
	if local.isManagedRuntimeRunning("telegram") {
		t.Fatal("telegram running = true, want false")
	}
}

func TestManagedRuntimeSupervisorSkipsSlackMissingToken(t *testing.T) {
	cases := []struct {
		name    string
		setNext func(*viper.Viper)
	}{
		{name: "missing bot token"},
		{
			name: "missing app token",
			setNext: func(v *viper.Viper) {
				v.Set("slack.bot_token", "xoxb-token")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			supervisor := newManagedRuntimeSupervisor(nil, false, false)
			reader := viper.New()
			reader.Set("console.managed_runtimes", []string{"slack"})
			if tc.setNext != nil {
				tc.setNext(reader)
			}

			prepared, err := supervisor.PrepareReload(reader)
			if err != nil {
				t.Fatalf("PrepareReload() error = %v, want nil", err)
			}
			if len(prepared.kinds) != 0 {
				t.Fatalf("prepared.kinds = %#v, want empty", prepared.kinds)
			}
			if len(prepared.children) != 0 {
				t.Fatalf("prepared.children len = %d, want 0", len(prepared.children))
			}
		})
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
