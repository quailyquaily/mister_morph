package consolecmd

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

func TestManagedRuntimeSupervisorStartSkipsConfigError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	supervisor := newManagedRuntimeSupervisor(nil, []string{"telegram"})
	if err := supervisor.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	supervisor.Close()
}
