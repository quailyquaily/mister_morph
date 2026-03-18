package consolecmd

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

func TestManagedRuntimeSupervisorStartSkipsConfigErrorOutsideSetupMode(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	supervisor := newManagedRuntimeSupervisor(nil, []string{"telegram"}, false)
	if err := supervisor.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	supervisor.Close()
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
