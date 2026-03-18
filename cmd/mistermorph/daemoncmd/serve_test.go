package daemoncmd

import "testing"

func TestNewServeCmdIncludesInspectFlags(t *testing.T) {
	cmd := NewServeCmd(ServeDependencies{})
	if cmd.Flags().Lookup("inspect-prompt") == nil {
		t.Fatal("inspect-prompt flag missing")
	}
	if cmd.Flags().Lookup("inspect-request") == nil {
		t.Fatal("inspect-request flag missing")
	}
	if cmd.Deprecated == "" {
		t.Fatal("serve command should be marked deprecated")
	}
	if flag := cmd.Flags().Lookup("server-listen"); flag == nil || flag.Deprecated == "" {
		t.Fatal("server-listen flag should be marked deprecated")
	}
}
