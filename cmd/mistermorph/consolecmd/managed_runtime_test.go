package consolecmd

import (
	"testing"

	"github.com/spf13/viper"
)

func TestManagedRuntimeSupervisorReloadRejectsInvalidConfigWithoutMutatingState(t *testing.T) {
	local := &consoleLocalRuntime{managedRuntimeRunning: map[string]bool{}}
	local.SetManagedRuntimeRunning("telegram", true)
	supervisor := newManagedRuntimeSupervisor(local, false, false)

	current := viper.New()
	current.Set("console.managed_runtimes", []string{"telegram"})
	current.Set("telegram.bot_token", "old-token")
	supervisor.configReader = current
	supervisor.kinds = []string{"telegram"}

	next := viper.New()
	next.Set("console.managed_runtimes", []string{"telegram"})

	err := supervisor.ReloadConfig(next)
	if err == nil {
		t.Fatal("ReloadConfig() error = nil, want invalid config error")
	}
	if got := supervisor.configReader.GetString("telegram.bot_token"); got != "old-token" {
		t.Fatalf("configReader.telegram.bot_token = %q, want %q", got, "old-token")
	}
	if len(supervisor.kinds) != 1 || supervisor.kinds[0] != "telegram" {
		t.Fatalf("kinds = %#v, want [telegram]", supervisor.kinds)
	}
	if !local.isManagedRuntimeRunning("telegram") {
		t.Fatal("telegram running = false, want unchanged true")
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
