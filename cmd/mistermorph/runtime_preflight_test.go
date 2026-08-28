package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestShouldRunRuntimeFilePreflightForMixin(t *testing.T) {
	root := &cobra.Command{Use: "mistermorph"}
	command := &cobra.Command{Use: "mixin"}
	root.AddCommand(command)
	if !shouldRunRuntimeFilePreflight(command) {
		t.Fatal("Mixin runtime skipped file preflight")
	}
	if got := command.CommandPath(); !strings.HasSuffix(got, " mixin") {
		t.Fatalf("CommandPath() = %q", got)
	}
}
