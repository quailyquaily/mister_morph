package consolecmd

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestManagedRuntimeSupervisorStartRequiresConfigOutsideSetupMode(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	supervisor := newManagedRuntimeSupervisor(nil, []string{"telegram"}, false)
	err := supervisor.Start(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "missing telegram.bot_token") {
		t.Fatalf("Start() error = %v, want missing telegram.bot_token", err)
	}
}

func TestManagedRuntimeSupervisorStartSkipsConfigErrorInSetupMode(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	supervisor := newManagedRuntimeSupervisor(nil, []string{"telegram"}, true)
	if err := supervisor.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	supervisor.Close()
}
