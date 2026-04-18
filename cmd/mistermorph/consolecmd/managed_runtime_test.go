package consolecmd

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

func TestManagedRuntimeSupervisorStartSkipsConfigError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("console.managed_runtimes", []string{"telegram"})

	supervisor := newManagedRuntimeSupervisor(nil, false, false)
	if err := supervisor.ReloadConfig(viper.GetViper()); err != nil {
		t.Fatalf("ReloadConfig() error = %v, want nil", err)
	}
	if err := supervisor.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	supervisor.Close()
}

func TestManagedRuntimeKindsFromReaderRejectsUnsupportedValue(t *testing.T) {
	v := viper.New()
	v.Set("console.managed_runtimes", []string{"telegram", "line"})

	_, err := managedRuntimeKindsFromReader(v)
	if err == nil || err.Error() == "" {
		t.Fatalf("managedRuntimeKindsFromReader() error = %v, want unsupported value", err)
	}
}
